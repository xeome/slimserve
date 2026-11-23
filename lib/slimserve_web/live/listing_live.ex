defmodule SlimServeWeb.ListingLive do
  @moduledoc false
  use SlimServeWeb, :live_view

  alias SlimServe.Root

  @impl true
  def mount(_params, %{"segments" => segments}, socket) do
    {:ok,
     socket
     |> assign(page_title: "/" <> Enum.join(segments, "/"), crumbs: crumbs(segments))
     |> stream(:entries, entries(segments))}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <h1 id="crumbs" class="crumbs">
        <a href={~p"/"}>root</a>
        <span :for={{name, href} <- @crumbs}>/ <a href={href}>{name}</a></span>
      </h1>

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
                <.icon
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
    </Layouts.app>
    """
  end

  # Exactly the names Root.resolve/2 would serve: the listing has no filter of its own.
  defp entries(segments) do
    root = Application.fetch_env!(:slimserve, :root)

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

  defp href(segments), do: "/" <> Enum.map_join(segments, "/", &URI.encode(&1, fn char -> URI.char_unreserved?(char) end))

  defp human(bytes) when bytes < 1024, do: "#{bytes} B"
  defp human(bytes), do: human(bytes / 1024, ~w(KB MB GB TB))

  defp human(size, [unit | rest]) when size < 1024 or rest == [],
    do: "#{:erlang.float_to_binary(size, decimals: 1)} #{unit}"

  defp human(size, [_unit | rest]), do: human(size / 1024, rest)
end
