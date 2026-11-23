defmodule SlimServeWeb.ListingLive do
  @moduledoc false
  use SlimServeWeb, :live_view

  alias SlimServe.Ignore
  alias SlimServe.Root
  alias SlimServe.Thumbnail
  alias SlimServeWeb.Login

  # A tab opened before the view toggle shipped reconnects with a session that has no "view".
  @impl true
  def mount(_params, %{"segments" => segments} = session, socket) do
    view = Map.get(session, "view", "list")
    root = Application.fetch_env!(:slimserve, :root)
    write? = Login.write?(session)

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
     |> assign(write?: write?, confirm: nil, mkdir_form: to_form(%{"name" => ""}), upload_form: to_form(%{}))
     |> attach_hook(:write_gate, :handle_event, &write_gate(&1, &2, &3, session))
     |> allow_files(write?)
     |> stream(:entries, entries)}
  end

  # Every event this page handles is a write, so the gate halts them all without write rights, and
  # one added later cannot skip it. The session is checked again on each event, so a changed or
  # removed write password stops an open page too.
  defp write_gate(_event, _params, socket, session) do
    if socket.assigns.write? and Login.write?(session), do: {:cont, socket}, else: {:halt, socket}
  end

  # Without write rights there is no upload config, so the upload channel has nothing to accept.
  defp allow_files(socket, false), do: socket

  defp allow_files(socket, true) do
    allow_upload(socket, :files,
      accept: Application.get_env(:slimserve, :upload_types, :any),
      max_file_size: Application.get_env(:slimserve, :upload_max_bytes, 100 * 1024 * 1024),
      max_entries: 20
    )
  end

  # Each write names an entry of this folder and is resolved again here. Its change reaches every
  # open page, this one too, through SlimServe.Watcher: no broadcast of its own.
  @impl true
  def handle_event("validate", _params, socket), do: {:noreply, socket}

  def handle_event("cancel_upload", %{"ref" => ref}, socket), do: {:noreply, cancel_upload(socket, :files, ref)}

  # Only finished entries: one refused for its size or type never finishes, and stays listed with its error.
  def handle_event("upload", _params, socket) do
    {done, _pending} = uploaded_entries(socket, :files)

    results =
      for entry <- done do
        consume_uploaded_entry(socket, entry, fn %{path: tmp} ->
          {:ok, put_upload(socket.assigns.segments, entry.client_name, tmp)}
        end)
      end

    {:noreply, if(Enum.all?(results, &(&1 == :ok)), do: socket, else: refused(socket))}
  end

  def handle_event("mkdir", %{"name" => name}, socket) do
    done(socket, with({:ok, abs} <- fresh(socket.assigns.segments, name, dir?: true), do: File.mkdir(abs)))
  end

  # The destination is a path from the root, so a rename and a move are one event.
  def handle_event("rename", %{"from" => from, "to" => to}, socket) do
    segments = socket.assigns.segments

    result =
      with {:ok, src, followed_kind, lstat_type} <- source(segments, from),
           {parent, [name]} <- to |> String.split("/", trim: true) |> Enum.split(-1),
           true <- followed_kind == :dir or upload_type?(name),
           {:ok, dest} <- fresh(parent, name, dir?: followed_kind == :dir),
           # A relative link's target would mean another path in another folder.
           true <- lstat_type != :symlink or Path.dirname(dest) == Path.dirname(src) do
        move(src, dest, lstat_type, segments ++ [from])
      end

    done(socket, result)
  end

  # A folder with anything in it waits for confirm_delete; File.lstat, not resolve, picks the call,
  # so a link is removed as a link, a link to a folder included.
  def handle_event("delete", %{"name" => name}, socket) do
    case source(socket.assigns.segments, name) do
      {:ok, abs, _followed_kind, :directory} ->
        case {File.ls(abs), confirm(name, abs)} do
          {{:ok, []}, _confirm} -> done(socket, File.rmdir(abs))
          {{:ok, _names}, %{} = confirm} -> {:noreply, assign(socket, confirm: confirm)}
          _error -> done(socket, :error)
        end

      {:ok, abs, _followed_kind, _lstat_type} ->
        done(socket, File.rm(abs))

      :error ->
        done(socket, :error)
    end
  end

  # The bar named a folder and a count, so a folder that is another one now, or holds more or fewer
  # files, is not removed: the bar asks again. File.rm_rf/1 removes a link inside the tree without
  # following it.
  def handle_event("confirm_delete", %{"name" => name}, %{assigns: %{confirm: %{name: name} = asked}} = socket) do
    with {:ok, abs, _followed_kind, :directory} <- source(socket.assigns.segments, name),
         now = confirm(name, abs),
         ^asked <- now,
         {:ok, _removed} <- File.rm_rf(abs) do
      done(assign(socket, confirm: nil), :ok)
    else
      %{} = now -> {:noreply, assign(socket, confirm: now)}
      _error -> done(assign(socket, confirm: nil), :error)
    end
  end

  def handle_event("confirm_delete", _params, socket), do: done(socket, :error)

  def handle_event("cancel_delete", _params, socket), do: {:noreply, assign(socket, confirm: nil)}

  defp confirm(name, abs) do
    with {:ok, %File.Stat{type: :directory, inode: inode}} <- File.lstat(abs),
         do: %{name: name, count: count_files(abs), inode: inode}
  end

  defp done(socket, :ok), do: {:noreply, socket}
  defp done(socket, _refused), do: {:noreply, refused(socket)}

  # One answer for every refusal, as for a 404: it never says which rule refused.
  defp refused(socket), do: put_flash(socket, :error, "That change was refused.")

  # Written under a dot name first, so a half-written file is never served. A hard link refuses an
  # existing name, so an upload never overwrites one, with no gap between a check and the write.
  defp put_upload(segments, name, tmp) do
    with true <- upload_type?(name), {:ok, dest} <- fresh(segments, name) do
      temp = Path.join(Path.dirname(dest), ".upload-" <> Base.url_encode64(:crypto.strong_rand_bytes(12)))

      try do
        with :ok <- File.cp(tmp, temp), do: File.ln(temp, dest)
      after
        File.rm(temp)
      end
    end
  end

  # allow_upload's accept also passes a file by the type the browser claims, so the name is checked here.
  defp upload_type?(name) do
    case Application.get_env(:slimserve, :upload_types, :any) do
      :any -> true
      types -> String.downcase(Path.extname(name)) in types
    end
  end

  # A file moves by a hard link, which refuses an existing name; File.rename/2 has no such flag.
  defp move(src, dest, :directory, requested) do
    if hidden_inside?(src, requested), do: :error, else: File.rename(src, dest)
  end

  defp move(src, dest, _type, _requested) do
    with :ok <- File.ln(src, dest), do: File.rm(src)
  end

  # Ignore patterns are relative to their ignore file, so a moved folder could reveal an entry one
  # hides at its current path. Dot-names stay hidden anywhere, so they are not looked at.
  # src is never the root itself, whose relative path would be ".": source/2 takes one plain name.
  defp hidden_inside?(src, requested) do
    root = root()
    resolved = Path.split(Path.relative_to(src, root))

    Enum.any?(visible_names(src), fn rel ->
      Enum.any?(Enum.uniq([requested ++ rel, resolved ++ rel]), &Ignore.ignored?(root, &1))
    end)
  end

  # Every non-dot path under dir, relative to it; links are not followed.
  defp visible_names(dir) do
    for name <- ls(dir), not String.starts_with?(name, "."), path = Path.join(dir, name), rest <- [[] | below(path)] do
      [name | rest]
    end
  end

  defp below(path), do: if(directory?(path), do: visible_names(path), else: [])

  # Every entry under dir that is not a folder, dot-files and links included, because the delete takes them all.
  defp count_files(dir) do
    for name <- ls(dir), path = Path.join(dir, name), reduce: 0 do
      n -> if directory?(path), do: n + count_files(path), else: n + 1
    end
  end

  defp directory?(path), do: match?({:ok, %File.Stat{type: :directory}}, File.lstat(path))

  defp ls(dir) do
    case File.ls(dir) do
      {:ok, names} -> names
      {:error, _reason} -> []
    end
  end

  # A new name: it passes the gate and nothing, not even a dangling link, is there yet.
  defp fresh(segments, name, opts \\ []) do
    with {:ok, abs} <- Root.target(root(), segments, name, opts),
         {:error, :enoent} <- File.lstat(abs) do
      {:ok, abs}
    else
      _ -> :error
    end
  end

  # An existing name: it passes the gate and is visible, as the listing shows it. followed_kind is
  # what resolve/2 sees through a link (:dir or :file); lstat_type is the entry's own type, :symlink for a link.
  defp source(segments, name) do
    with {:ok, abs} <- Root.target(root(), segments, name),
         {followed_kind, _path} when followed_kind in [:dir, :file] <- Root.resolve(root(), segments ++ [name]),
         {:ok, %File.Stat{type: lstat_type}} <- File.lstat(abs) do
      {:ok, abs, followed_kind, lstat_type}
    else
      _ -> :error
    end
  end

  defp root, do: Application.fetch_env!(:slimserve, :root)

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
    <Layouts.app flash={@flash} write?={@write?}>
      <:breadcrumbs>
        <a href={~p"/"} aria-current={@segments == [] && "page"}>Root</a>
        <span :for={{name, path} <- @crumbs} class="crumb">
          <.icon name="hero-chevron-right" class="size-3" />
          <a href={path} aria-current={path == href(@segments) && "page"}>{name}</a>
        </span>
      </:breadcrumbs>
      <div id="listing" data-view={@view}>
        <div class={["listing-bar", @segments != [] && "folder-heading"]}>
          <h1>{List.last(@segments) || "Files"}</h1>
          <nav class="segmented" aria-label="View">
            <a
              :for={{view, label} <- [{"list", "List"}, {"grid", "Grid"}]}
              id={"view-" <> view}
              href={href(@segments) <> "?view=" <> view}
              aria-current={@view == view && "true"}
            >
              <.icon
                name={if(view == "list", do: "hero-list-bullet", else: "hero-squares-2x2")}
                class="size-4"
              />
              {label}
            </a>
          </nav>
        </div>

        <section :if={@write?} id="write-bar" class="write-bar">
          <.form
            for={@upload_form}
            id="upload-form"
            class="dropzone"
            phx-change="validate"
            phx-submit="upload"
            phx-drop-target={@uploads.files.ref}
          >
            <label class="btn">
              <.icon name="hero-arrow-up-tray" class="size-4" /> Choose files
              <.live_file_input upload={@uploads.files} class="sr-only" />
            </label>
            <span class="sub">or drop them here</span>
            <button type="submit" class="btn primary" id="upload-submit">Upload</button>
          </.form>
          <.form for={@mkdir_form} id="mkdir-form" class="mkdir" phx-submit="mkdir">
            <.input
              field={@mkdir_form[:name]}
              placeholder="New folder"
              aria-label="New folder name"
              required
            />
            <button type="submit" class="btn">
              <.icon name="hero-folder-plus" class="size-4" /> Create
            </button>
          </.form>
        </section>

        <ul
          :if={@write? and (@uploads.files.entries != [] or upload_errors(@uploads.files) != [])}
          id="upload-entries"
          class="upload-entries"
        >
          <li :for={err <- upload_errors(@uploads.files)} class="field-error">{upload_error(err)}</li>
          <li :for={entry <- @uploads.files.entries} id={"upload-" <> entry.ref}>
            <div class="upload-status">
              <span class="label">{entry.client_name}</span>
              <progress
                max="100"
                value={entry.progress}
                aria-label={"Upload progress for " <> entry.client_name}
              />
            </div>
            <span :if={entry.valid?} class="sub">
              {cond do
                entry.done? -> "Finishing…"
                entry.preflighted? -> "Uploading #{entry.progress}%"
                true -> "Ready to upload"
              end}
            </span>
            <span :for={err <- upload_errors(@uploads.files, entry)} class="field-error">
              {upload_error(err)}
            </span>
            <button
              type="button"
              id={"upload-remove-" <> entry.ref}
              class="btn"
              title="Remove"
              phx-click="cancel_upload"
              phx-value-ref={entry.ref}
            >
              <.icon name="hero-x-mark" class="size-4" />
              <span class="sr-only">Remove {entry.client_name}</span>
            </button>
          </li>
        </ul>

        <div
          :if={@confirm}
          id="confirm-delete"
          class="confirm-bar"
          role="alert"
          data-count={@confirm.count}
        >
          <span>
            Delete folder <strong>{@confirm.name}</strong>
            and its {@confirm.count} {if(@confirm.count == 1,
              do: "file",
              else: "files"
            )}?
          </span>
          <div class="confirm-actions">
            <button
              type="button"
              id="confirm-delete-yes"
              class="btn primary"
              phx-click="confirm_delete"
              phx-value-name={@confirm.name}
            >
              Delete
            </button>
            <button
              type="button"
              id="confirm-delete-cancel"
              class="btn"
              phx-click="cancel_delete"
              phx-mounted={JS.focus()}
            >
              Cancel
            </button>
          </div>
        </div>

        <table class="table listing" inert={not is_nil(@confirm)}>
          <thead>
            <tr>
              <th>Name</th>
              <th class="num">Size</th>
              <th class="num when">Modified (UTC)</th>
              <th :if={@write?}><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody id="entries" phx-update="stream">
            <tr id="entries-empty" class="empty">
              <td colspan={if(@write?, do: 4, else: 3)}>
                <.icon name="hero-folder-open" class="size-6" />
                <strong>This folder is empty.</strong>
              </td>
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
              <td :if={@write?} class="actions">
                <details class="move">
                  <summary class="btn" title="Rename or move">
                    <.icon name="hero-pencil-square" class="size-4" />
                    <span class="sr-only">Rename or move {entry.name}</span>
                  </summary>
                  <.form
                    for={to_form(%{})}
                    id={"move-" <> entry.id}
                    phx-submit="rename"
                    data-name={entry.name}
                    class="move-form"
                  >
                    <.input type="hidden" name="from" value={entry.name} />
                    <.input
                      name="to"
                      id={"move-to-" <> entry.id}
                      value={entry.rel}
                      label="New path from root"
                      aria-describedby={"move-example-" <> entry.id}
                      required
                    />
                    <button type="submit" class="btn primary">Move</button>
                    <p id={"move-example-" <> entry.id} class="sub move-example">
                      Example: {if(entry.dir?, do: "Projects/Archive", else: "Projects/report.pdf")}
                    </p>
                  </.form>
                </details>
                <button
                  type="button"
                  id={"delete-" <> entry.id}
                  class="btn"
                  title="Delete"
                  phx-click="delete"
                  phx-value-name={entry.name}
                >
                  <.icon name="hero-trash" class="size-4" />
                  <span class="sr-only">Delete {entry.name}</span>
                </button>
              </td>
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
            # The row's DOM id: from the name, so a relist never patches a row with another entry.
            id: Base.url_encode64(name, padding: false),
            name: name,
            href: href(path),
            rel: Enum.join(path, "/"),
            # The same rule as the thumbnail route, so the listing asks only for thumbnails it can get.
            thumb:
              if(kind == :file and Thumbnail.image?(abs) and stat.size <= thumb_max_file_bytes,
                do: href(path) <> "?thumb=1&v=#{stat.mtime}-#{stat.size}"
              ),
            size: if(kind == :file, do: human(stat.size)),
            modified: stat.mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d %H:%M")
          }
        end

      Enum.sort_by(entries, &{not &1.dir?, &1.name})
    else
      _ -> []
    end
  end

  defp upload_error(:too_large), do: "Larger than the upload limit"
  defp upload_error(:not_accepted), do: "Not an accepted file type"
  defp upload_error(:too_many_files), do: "Too many files at once"
  defp upload_error(_error), do: "Upload failed"

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
