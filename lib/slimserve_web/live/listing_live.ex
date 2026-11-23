defmodule SlimServeWeb.ListingLive do
  @moduledoc false
  use SlimServeWeb, :live_view

  alias SlimServe.Root
  alias SlimServe.Thumbnail

  # A tab opened before the view toggle shipped reconnects with a session that has no "view".
  @impl true
  def mount(_params, %{"segments" => segments} = session, socket) do
    view = Map.get(session, "view", "list")
    root = Application.fetch_env!(:slimserve, :root)

    # The Watcher names the directory a symlink leads to, not the link.
    with true <- connected?(socket), {:dir, dir} <- Root.resolve(root, segments) do
      Phoenix.PubSub.subscribe(SlimServe.PubSub, Root.topic(Path.relative_to(dir, root)))
    end

    entries = entries(segments)

    {:ok,
     socket
     |> assign(
       segments: segments,
       view: view,
       relist?: false,
       page_title: "/" <> Enum.join(segments, "/"),
       crumbs: crumbs(segments)
     )
     |> assign(entries_hash: :erlang.phash2(entries))
     |> stream(:entries, entries)}
  end

  # SlimServe.Watcher sends one message per file event: a burst re-lists once, 100 ms after its first event.
  @impl true
  def handle_info({:dir_changed, _dir}, %{assigns: %{relist?: true}} = socket), do: {:noreply, socket}

  def handle_info({:dir_changed, _dir}, socket) do
    Process.send_after(self(), :relist, 100)
    {:noreply, assign(socket, relist?: true)}
  end

  # A directory removed or newly ignored sends the browser to the nearest directory still served.
  def handle_info(:relist, socket) do
    segments = socket.assigns.segments
    socket = assign(socket, relist?: false)

    case nearest_dir(Application.fetch_env!(:slimserve, :root), segments) do
      ^segments -> {:noreply, relist(socket, entries(segments))}
      ancestor -> {:noreply, redirect(socket, to: href(ancestor))}
    end
  end

  # An event on a hidden or ignored file lists the same entries: send nothing.
  defp relist(socket, entries) do
    hash = :erlang.phash2(entries)

    if hash == socket.assigns.entries_hash,
      do: socket,
      else: socket |> assign(entries_hash: hash) |> stream(:entries, entries, reset: true)
  end

  defp nearest_dir(root, segments) do
    case Root.resolve(root, segments) do
      {:dir, _dir} -> segments
      _ -> nearest_dir(root, Enum.drop(segments, -1))
    end
  end

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <div id="listing" data-view={@view}>
        <div class="listing-bar">
          <h1 id="crumbs" class="crumbs">
            <a href={~p"/"}>root</a>
            <span :for={{name, href} <- @crumbs}>/ <a href={href}>{name}</a></span>
          </h1>
          <nav class="segmented" aria-label="View">
            <a
              :for={{view, label} <- [{"list", "List"}, {"grid", "Grid"}]}
              id={"view-" <> view}
              href={href(@segments) <> "?view=" <> view}
              aria-current={@view == view && "true"}
            >
              {label}
            </a>
          </nav>
        </div>

        <table class="table listing">
          <thead>
            <tr>
              <th>Name</th>
              <th class="num">Size</th>
              <th class="num when">Modified (UTC)</th>
            </tr>
          </thead>
          <tbody id="entries" phx-update="stream">
            <tr id="entries-empty" class="empty">
              <td colspan="3">This folder is empty.</td>
            </tr>
            <tr :for={{id, entry} <- @streams.entries} id={id}>
              <td class="name">
                <a href={entry.href}>
                  <img :if={entry.thumb} class="thumb" src={entry.thumb} alt="" loading="lazy" />
                  <.icon
                    :if={!entry.thumb}
                    name={if(entry.dir?, do: "hero-folder", else: "hero-document")}
                    class={["size-4 kind", entry.dir? && "dir"]}
                  />
                  <span class="sr-only">{if(entry.dir?, do: "Folder", else: "File")}</span>
                  <span class="label">{entry.name}</span>
                </a>
              </td>
              <td class="num">{entry.size}</td>
              <td class="num when">{entry.modified}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </Layouts.app>
    """
  end

  # Exactly the names Root.resolve/2 would serve: the listing has no filter of its own.
  defp entries(segments) do
    root = Application.fetch_env!(:slimserve, :root)
    thumb_max_file_bytes = Application.fetch_env!(:slimserve, :thumb_max_file_bytes)

    # File.ls/1 skips names that are not valid UTF-8 (spec: Directory listing).
    with {:dir, dir} <- Root.resolve(root, segments),
         {:ok, names} <- File.ls(dir) do
      # ponytail: resolve/2 re-reads the ancestors' ignore files for every name.
      # Load them once per listing if big folders get slow.
      entries =
        for name <- names,
            path = segments ++ [name],
            {kind, abs} <- [Root.resolve(root, path)],
            {:ok, stat} <- [File.stat(abs, time: :posix)] do
          %{
            dir?: kind == :dir,
            name: name,
            href: href(path),
            # The same rule as the thumbnail route, so the listing asks only for thumbnails it can get.
            thumb:
              if(kind == :file and Thumbnail.image?(abs) and stat.size <= thumb_max_file_bytes,
                do: href(path) <> "?thumb=1&v=#{stat.mtime}-#{stat.size}"
              ),
            size: if(kind == :file, do: human(stat.size)),
            modified: stat.mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d %H:%M")
          }
        end

      entries
      |> Enum.sort_by(&{not &1.dir?, &1.name})
      |> Enum.with_index(&Map.put(&1, :id, &2))
    else
      _ -> []
    end
  end

  defp crumbs(segments) do
    segments
    |> Enum.scan([], &(&2 ++ [&1]))
    |> Enum.map(&{List.last(&1), href(&1)})
  end

  def href(segments), do: "/" <> Enum.map_join(segments, "/", &URI.encode(&1, fn char -> URI.char_unreserved?(char) end))

  defp human(bytes) when bytes < 1024, do: "#{bytes} B"
  defp human(bytes), do: human(bytes / 1024, ~w(KB MB GB TB))

  defp human(size, [unit | rest]) when size < 1024 or rest == [],
    do: "#{:erlang.float_to_binary(size, decimals: 1)} #{unit}"

  defp human(size, [_unit | rest]), do: human(size / 1024, rest)
end
