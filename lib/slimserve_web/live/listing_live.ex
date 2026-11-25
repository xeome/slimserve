defmodule SlimServeWeb.ListingLive do
  @moduledoc false
  use SlimServeWeb, :live_view

  alias SlimServe.Storage
  alias SlimServeWeb.Login

  # S3 has no watcher, so an open S3 listing re-lists on this timer to show changes made outside the app.
  @poll_ms 30_000

  # A tab opened before the view toggle shipped reconnects with a session that has no "view".
  @impl true
  def mount(_params, %{"segments" => segments} = session, socket) do
    view = Map.get(session, "view", "list")
    write? = Login.write?(session)

    if connected?(socket) do
      Storage.subscribe(segments)
      if Storage.s3?(), do: poll()
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

  # Each write names an entry of this folder and is resolved again by SlimServe.Storage. Its change
  # reaches every open page, this one too, through the backend's own messages.
  @impl true
  def handle_event("validate", _params, socket), do: {:noreply, socket}

  def handle_event("cancel_upload", %{"ref" => ref}, socket), do: {:noreply, cancel_upload(socket, :files, ref)}

  # Only finished entries: one refused for its size or type never finishes, and stays listed with its error.
  def handle_event("upload", _params, socket) do
    {done, _pending} = uploaded_entries(socket, :files)

    results =
      for entry <- done do
        consume_uploaded_entry(socket, entry, fn %{path: tmp} ->
          {:ok, Storage.upload(socket.assigns.segments, entry.client_name, tmp)}
        end)
      end

    {:noreply, if(Enum.all?(results, &(&1 == :ok)), do: socket, else: refused(socket))}
  end

  def handle_event("mkdir", %{"name" => name}, socket), do: done(socket, Storage.mkdir(socket.assigns.segments, name))

  # The destination is a path from the root, so a rename and a move are one event.
  def handle_event("rename", %{"from" => from, "to" => to}, socket),
    do: done(socket, Storage.move(socket.assigns.segments, from, to))

  def handle_event("delete", %{"name" => name}, socket) do
    case Storage.delete(socket.assigns.segments, name) do
      {:confirm, confirm} -> {:noreply, assign(socket, confirm: confirm)}
      result -> done(socket, result)
    end
  end

  def handle_event("confirm_delete", %{"name" => name}, %{assigns: %{confirm: %{name: name} = asked}} = socket) do
    case Storage.confirm_delete(socket.assigns.segments, name, asked) do
      {:confirm, now} -> {:noreply, assign(socket, confirm: now)}
      result -> done(assign(socket, confirm: nil), result)
    end
  end

  def handle_event("confirm_delete", _params, socket), do: done(socket, :error)

  def handle_event("cancel_delete", _params, socket), do: {:noreply, assign(socket, confirm: nil)}

  defp done(socket, :ok), do: {:noreply, socket}
  # S3 has no atomic folder write: some keys changed and others did not, and nothing is rolled back.
  defp done(socket, :partial), do: {:noreply, put_flash(socket, :error, "Part of that change was refused.")}
  defp done(socket, _refused), do: {:noreply, refused(socket)}

  # One answer for every refusal, as for a 404: it never says which rule refused.
  defp refused(socket), do: put_flash(socket, :error, "That change was refused.")

  # SlimServe.Watcher sends one message per file event.
  # S3 sends one per changed folder, and {:tree_changed, rel} for a moved or deleted folder and all below it.
  @impl true
  def handle_info({:tree_changed, rel}, socket) do
    own = Enum.join(socket.assigns.segments, "/")

    if own == rel or String.starts_with?(own, rel <> "/"),
      do: handle_info({:dir_changed, own}, socket),
      else: {:noreply, socket}
  end

  def handle_info({:dir_changed, _dir}, %{assigns: %{relist?: true}} = socket), do: {:noreply, socket}

  def handle_info({:dir_changed, _dir}, socket) do
    Process.send_after(self(), :relist, 100)
    {:noreply, assign(socket, relist?: true)}
  end

  def handle_info(:relist, socket), do: {:noreply, socket |> assign(relist?: false) |> relist([])}

  # The poll skips the metadata cache, so a change made outside the app shows within one period.
  def handle_info(:poll, socket) do
    poll()
    {:noreply, relist(socket, fresh: true)}
  end

  defp poll, do: Process.send_after(self(), :poll, Application.get_env(:slimserve, :s3_poll_ms, @poll_ms))

  # An outage is not an empty folder.
  defp relist(socket, opts) do
    segments = socket.assigns.segments

    case Storage.list(segments, opts) do
      {:ok, entries} ->
        put_entries(socket, Enum.map(entries, &entry(segments, &1)))

      {:error, _reason} ->
        socket

      :error ->
        case nearest_dir(segments) do
          ^segments -> put_entries(socket, [])
          {:error, _reason} -> socket
          ancestor -> redirect(socket, to: href(ancestor))
        end
    end
  end

  # An event on a hidden or ignored file lists the same entries: send nothing.
  defp put_entries(socket, entries) do
    hash = :erlang.phash2(entries)

    if hash == socket.assigns.entries_hash,
      do: socket,
      else: socket |> assign(entries_hash: hash) |> stream(:entries, entries, reset: true)
  end

  defp nearest_dir(segments) do
    case Storage.resolve(segments) do
      {:dir, _dir} -> segments
      {:error, _reason} = error -> error
      _ -> nearest_dir(Enum.drop(segments, -1))
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

  defp entries(segments) do
    case Storage.list(segments) do
      {:ok, entries} -> Enum.map(entries, &entry(segments, &1))
      _ -> []
    end
  end

  defp entry(segments, entry) do
    path = segments ++ [entry.name]

    %{
      dir?: entry.dir?,
      # The row's DOM id: from the name, so a relist never patches a row with another entry.
      id: Base.url_encode64(entry.name, padding: false),
      name: entry.name,
      href: href(path),
      rel: Enum.join(path, "/"),
      thumb: entry.thumb && href(path) <> "?thumb=1&v=" <> URI.encode_www_form(entry.thumb),
      size: if(not entry.dir?, do: human(entry.size)),
      # An S3 folder is a key prefix, which has no time of its own.
      modified: entry.mtime && entry.mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d %H:%M")
    }
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
