defmodule SlimServeWeb.ListingLive do
  @moduledoc false
  use SlimServeWeb, :live_view

  alias SlimServe.Kind
  alias SlimServe.Root
  alias SlimServe.Storage
  alias SlimServe.ThumbCache
  alias SlimServeWeb.ListingLive.Tree
  alias SlimServeWeb.Login

  # S3 has no watcher, so an open S3 listing re-lists on this timer to show changes made outside the app.
  @poll_ms 30_000

  @reads ~w(select deselect filter open tree_expand)

  # A tab opened before the view toggle shipped reconnects with a session that has no "view".
  @impl true
  def mount(_params, %{"segments" => segments} = session, socket) do
    view = Map.get(session, "view", "list")
    write? = Login.write?(session)

    if connected?(socket) and Storage.s3?(), do: poll()

    {:ok,
     socket
     |> assign(
       segments: segments,
       view: view,
       rail: session["rail"],
       changed: [],
       tree: %{},
       expanded: [],
       watch: %{},
       topics: [],
       page_title: "/" <> Enum.join(segments, "/"),
       crumbs: crumbs(segments)
     )
     |> assign(raw: nil, entries_hash: nil, filter: "", filter_form: to_form(%{"filter" => ""}), selected: nil)
     |> assign(
       write?: write?,
       confirm: nil,
       confirm_path: nil,
       mkdir_form: to_form(%{"name" => ""}),
       upload_form: to_form(%{})
     )
     |> assign(
       picker: %{},
       target: nil,
       moving: nil,
       moving_in: nil,
       rename_form: to_form(%{"name" => ""}),
       move_form: to_form(%{})
     )
     |> attach_hook(:write_gate, :handle_event, &write_gate(&1, &2, &3, session))
     |> allow_files(write?)
     |> mount_listing()}
  end

  defp mount_listing(socket) do
    case list(socket, socket.assigns.segments) do
      {socket, {:ok, raw}} -> put_raw(socket, raw)
      {socket, _refused} -> put_raw(socket, [])
    end
  end

  # Deny by default: one added later cannot skip it. The session is checked again on each event, so a
  # changed or removed write password stops an open page too.
  defp write_gate(event, _params, socket, _session) when event in @reads, do: {:cont, socket}

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

  # Stream rows re-render only when inserted again. The old row goes in update_only, so a row the
  # filter hid stays hidden.
  @impl true
  def handle_event("select", %{"name" => name}, socket) do
    case Enum.find(socket.assigns.raw, &(&1.name == name)) do
      nil -> {:noreply, socket}
      entry -> {:noreply, select(socket, inspected(socket, entry))}
    end
  end

  # A folder link's hook asks for the folder, and history follows only on {ok: true}. A refusal
  # makes the client load the URL, so BrowseController answers it as it always has.
  def handle_event("open", %{"path" => path}, socket) when is_binary(path) do
    with {:ok, segments} <- folder(path),
         {socket, {:ok, raw}} <- list(socket, segments) do
      {:reply, %{ok: true}, open(socket, segments, raw)}
    else
      {socket, _refused} -> {:reply, %{ok: false}, watch(socket)}
      _refused -> {:reply, %{ok: false}, socket}
    end
  end

  def handle_event("open", _params, socket), do: {:reply, %{ok: false}, socket}

  def handle_event("tree_expand", %{"path" => path}, socket) when is_binary(path) do
    with {:ok, segments} <- folder(path),
         {socket, {:ok, raw}} <- list(socket, segments) do
      socket = assign(socket, expanded: Enum.uniq([segments | socket.assigns.expanded]))
      {:reply, %{ok: true}, socket |> level(segments, Tree.dirs(raw), []) |> put_tree()}
    else
      {socket, _refused} -> {:reply, %{ok: false}, watch(socket)}
      _refused -> {:reply, %{ok: false}, socket}
    end
  end

  def handle_event("tree_expand", _params, socket), do: {:reply, %{ok: false}, socket}

  def handle_event("deselect", _params, socket), do: {:noreply, select(socket, nil)}

  def handle_event("filter", params, socket) do
    filter = Map.get(params, "filter", "")
    {:noreply, socket |> assign(filter: filter, filter_form: to_form(%{"filter" => filter})) |> stream_entries()}
  end

  # Each write names its target by its path from the root, taken where the user acted, so one queued
  # behind an open still reaches it. SlimServe.Storage resolves it again. Its change reaches every
  # open page, this one too, through the backend's own messages.
  def handle_event("validate", _params, socket), do: {:noreply, socket}

  def handle_event("cancel_upload", %{"ref" => ref}, socket), do: {:noreply, cancel_upload(socket, :files, ref)}

  def handle_event("upload", %{"path" => path}, socket) do
    case folder(path) do
      {:ok, segments} -> {:noreply, put_uploads(socket, segments)}
      refused -> done(socket, refused)
    end
  end

  def handle_event("mkdir", %{"path" => path, "name" => name}, socket),
    do: done(socket, with({:ok, segments} <- folder(path), do: Storage.mkdir(segments, name)))

  # The destination is a path from the root, so a rename and a move are one event.
  def handle_event("rename", %{"path" => path, "to" => to}, socket) do
    case target(path) do
      {:ok, segments, from} ->
        if String.split(to, "/", trim: true) == segments ++ [from],
          do: {:noreply, socket},
          else: done(socket, Storage.move(segments, from, to))

      refused ->
        done(socket, refused)
    end
  end

  def handle_event("rename", %{"path" => path, "name" => name}, socket) do
    with {:ok, segments, from} <- target(path),
         false <- from == name,
         true <- Root.name?(name),
         :ok <- Storage.move(segments, from, Enum.join(segments ++ [name], "/")) do
      %{segments: open, selected: selected} = socket.assigns
      following? = segments == open and selected != nil and selected.name == from
      # Listed now, so the inspector follows the new name without waiting for the change message.
      socket = relist(socket, [])
      entry = following? && Enum.find(socket.assigns.raw, &(&1.name == name))
      {:noreply, if(entry, do: select(socket, inspected(socket, entry)), else: socket)}
    else
      true -> {:noreply, socket}
      result -> done(socket, result)
    end
  end

  def handle_event("move_open", %{"path" => path}, socket) do
    with {:ok, segments, name} <- target(path),
         {kind, _found} when kind in [:dir, :file] <- Storage.resolve(segments ++ [name]) do
      {:noreply,
       socket
       |> assign(moving: name, moving_in: segments, picker: socket.assigns.tree, target: nil)
       |> push_event("move_ready", %{})}
    else
      _refused -> {:noreply, socket}
    end
  end

  def handle_event("move_expand", %{"path" => path}, socket) do
    path = String.split(path, "/", trim: true)
    picker = socket.assigns.picker

    picker =
      if Map.has_key?(picker, path) and path != [], do: Map.delete(picker, path), else: Tree.expand(picker, path)

    {:noreply, assign(socket, picker: picker)}
  end

  def handle_event("move_pick", %{"path" => path}, socket),
    do: {:noreply, assign(socket, target: String.split(path, "/", trim: true))}

  def handle_event("delete", %{"path" => path}, socket) do
    with {:ok, segments, name} <- target(path),
         {:confirm, confirm} <- Storage.delete(segments, name) do
      {:noreply, assign(socket, confirm: confirm, confirm_path: path)}
    else
      result -> done(socket, result)
    end
  end

  def handle_event("confirm_delete", %{"path" => path}, %{assigns: %{confirm: %{} = asked, confirm_path: path}} = socket) do
    {:ok, segments, name} = target(path)

    case Storage.confirm_delete(segments, name, asked) do
      {:confirm, now} -> {:noreply, assign(socket, confirm: now)}
      result -> done(assign(socket, confirm: nil), result)
    end
  end

  def handle_event("confirm_delete", _params, socket), do: done(socket, :error)

  def handle_event("cancel_delete", _params, socket), do: {:noreply, assign(socket, confirm: nil)}

  # One refused for its size or type never finishes, and stays listed with its error.
  defp put_uploads(socket, segments) do
    {done, _pending} = uploaded_entries(socket, :files)

    results =
      for entry <- done do
        consume_uploaded_entry(socket, entry, fn %{path: tmp} ->
          {:ok, Storage.upload(segments, entry.client_name, tmp)}
        end)
      end

    if Enum.all?(results, &(&1 == :ok)), do: socket, else: refused(socket)
  end

  defp done(socket, :ok), do: {:noreply, socket}
  defp done(socket, false), do: {:noreply, refused(socket)}
  # S3 has no atomic folder write: some keys changed and others did not, and nothing is rolled back.
  defp done(socket, :partial), do: {:noreply, put_flash(socket, :error, "Part of that change was refused.")}
  defp done(socket, _refused), do: {:noreply, refused(socket)}

  # One answer for every refusal, as for a 404: it never says which rule refused.
  defp refused(socket), do: put_flash(socket, :error, "That change was refused.")

  # SlimServe.Watcher sends one message per file event.
  # S3 sends one per changed folder, and {:tree_changed, rel} for a moved or deleted folder and all below it.
  @impl true
  def handle_info({:tree_changed, rel}, socket) do
    under = String.split(rel, "/", trim: true)
    changes(socket, for({folder, _topics} <- socket.assigns.watch, below?(folder, under), do: folder))
  end

  # A folder reached through a symlink is watched under the topic of its target.
  def handle_info({:dir_changed, dir}, socket) do
    topic = Root.topic(dir)
    changes(socket, for({folder, topics} <- socket.assigns.watch, topic in topics, do: folder))
  end

  def handle_info(:relist, socket) do
    changed = retargeted(socket, socket.assigns.changed, socket.assigns.changed)
    # A changed folder may lead somewhere else now.
    socket = socket |> assign(changed: [], watch: Map.drop(socket.assigns.watch, changed)) |> subscribe(changed)
    socket = if socket.assigns.segments in changed, do: relist(socket, []), else: socket
    {:noreply, relevel(socket, changed)}
  end

  # The poll skips the metadata cache, so a change made outside the app shows within one period.
  def handle_info(:poll, socket) do
    poll()
    {:noreply, relist(socket, fresh: true)}
  end

  defp poll, do: Process.send_after(self(), :poll, Application.get_env(:slimserve, :s3_poll_ms, @poll_ms))

  defp changes(socket, []), do: {:noreply, socket}

  defp changes(%{assigns: %{changed: []}} = socket, folders) do
    Process.send_after(self(), :relist, 100)
    {:noreply, assign(socket, changed: folders)}
  end

  defp changes(socket, folders), do: {:noreply, assign(socket, changed: Enum.uniq(folders ++ socket.assigns.changed))}

  defp below?(path, folder), do: Enum.take(path, length(folder)) == folder

  # Retargeting a symlink signals only the folder it sits in.
  defp retargeted(_socket, [], changed), do: changed

  defp retargeted(socket, levels, changed) do
    %{tree: tree, watch: watch} = socket.assigns

    moved =
      for path <- levels,
          f <- Map.get(tree, path, []),
          folder = path ++ [f.name],
          folder not in changed,
          Storage.topics(folder) != watch[folder],
          uniq: true,
          do: folder

    retargeted(socket, moved, changed ++ moved)
  end

  # The open folder's level is put by its relist.
  defp relevel(socket, changed) do
    %{tree: tree, segments: open, counts: counts} = socket.assigns

    {socket, gone} =
      Enum.reduce(tree, {socket, []}, fn {path, folders}, {socket, gone} ->
        {socket, listed} = if path != open and path in changed, do: list(socket, path), else: {socket, :unchanged}

        case listed do
          {:ok, entries} -> {level(socket, path, Tree.dirs(entries), changed), gone}
          :error -> {socket, [path | gone]}
          _unchanged_or_outage -> {level(socket, path, Enum.map(folders, & &1.name), changed), gone}
        end
      end)

    tree = Map.reject(socket.assigns.tree, fn {path, _level} -> Enum.any?(gone, &below?(path, &1)) end)
    socket = assign(socket, tree: tree, expanded: Enum.filter(socket.assigns.expanded, &Map.has_key?(tree, &1)))
    socket = put_tree(socket)
    if socket.assigns.counts == counts, do: socket, else: stream_entries(socket)
  end

  defp level(socket, path, names, changed) do
    socket = subscribe(socket, for(name <- names, do: path ++ [name]))
    assign(socket, tree: Tree.put(socket.assigns.tree, path, names, changed))
  end

  defp put_tree(socket) do
    counts = Map.new(Map.get(socket.assigns.tree, socket.assigns.segments, []), &{&1.name, &1.count})
    watch(assign(socket, counts: counts))
  end

  # So a change landing meanwhile still arrives.
  defp list(socket, path, opts \\ []) do
    socket = subscribe(socket, [path])
    {socket, Storage.list(path, opts)}
  end

  # So each stays as live as the open folder. Subscribing asks the store nothing.
  defp subscribe(socket, folders) do
    new = Enum.reject(folders, &Map.has_key?(socket.assigns.watch, &1))

    if connected?(socket) and new != [],
      do: resubscribe(socket, Map.merge(socket.assigns.watch, Map.new(new, &{&1, Storage.topics(&1)}))),
      else: socket
  end

  defp watch(socket) do
    folders =
      for {path, level} <- socket.assigns.tree,
          folder <- [path | for(f <- level, do: path ++ [f.name])],
          uniq: true,
          do: folder

    resubscribe(socket, Map.take(socket.assigns.watch, folders))
  end

  defp resubscribe(socket, watch) do
    topics = watch |> Map.values() |> List.flatten() |> Enum.uniq()
    :ok = Storage.resubscribe(socket.assigns.topics, topics)
    assign(socket, watch: watch, topics: topics)
  end

  # An outage is not an empty folder.
  defp relist(socket, opts) do
    segments = socket.assigns.segments
    {socket, listed} = list(socket, segments, opts)

    case listed do
      {:ok, raw} ->
        put_raw(socket, raw)

      {:error, _reason} ->
        socket

      :error ->
        case nearest_dir(segments) do
          ^segments -> put_raw(socket, [])
          {:error, _reason} -> socket
          ancestor -> redirect(socket, to: href(ancestor))
        end
    end
  end

  # The hash covers the store's own entries only, never a rendered or time-relative string.
  defp put_raw(socket, raw) do
    hash = :erlang.phash2(raw)

    if hash == socket.assigns.entries_hash do
      socket
    else
      socket
      |> assign(raw: raw, entries_hash: hash)
      |> put_levels(raw)
      |> put_tree()
      |> assign(meta: meta(raw), card: card(raw))
      |> reselect()
      |> stream_entries()
    end
  end

  # So the tree is in the first paint: the owner took a second build per load for that.
  defp put_levels(socket, raw) do
    segments = socket.assigns.segments

    socket =
      for n <- 0..(length(segments) - 1)//1,
          path = Enum.take(segments, n),
          not Map.has_key?(socket.assigns.tree, path),
          reduce: socket do
        socket ->
          case list(socket, path) do
            {socket, {:ok, listed}} -> level(socket, path, Tree.dirs(listed), [])
            {socket, _refused} -> socket
          end
      end

    level(socket, segments, Tree.dirs(raw), [])
  end

  defp reselect(socket) do
    name = socket.assigns.selected && socket.assigns.selected.name
    entry = name && Enum.find(socket.assigns.raw, &(&1.name == name))
    selected = entry && inspected(socket, entry)
    assign(socket, selected: selected, rename_form: to_form(%{"name" => selected && selected.name}))
  end

  defp stream_entries(socket) do
    query = String.downcase(socket.assigns.filter)

    rows =
      for entry <- socket.assigns.raw,
          query == "" or String.contains?(String.downcase(entry.name), query),
          do: row(socket, entry)

    stream(socket, :entries, rows, reset: true)
  end

  defp select(socket, selected) do
    old = socket.assigns.selected

    socket
    |> assign(selected: selected, target: nil, rename_form: to_form(%{"name" => selected && selected.name}))
    |> restream(old)
    |> restream(selected)
  end

  defp restream(socket, nil), do: socket

  defp restream(socket, %{name: name}) do
    case Enum.find(socket.assigns.raw, &(&1.name == name)) do
      nil -> socket
      entry -> stream_insert(socket, :entries, row(socket, entry), update_only: true)
    end
  end

  # Splits and decodes as the router does (Plug.Router.Utils.decode_path_info!/1). The Storage call
  # that takes the segments refuses any it would not serve.
  defp folder(path) when is_binary(path) do
    {:ok, path |> Plug.Router.Utils.split() |> Enum.map(&URI.decode/1)}
  rescue
    # A malformed escape, which the router answers with a 400.
    ArgumentError -> :error
  end

  defp folder(_path), do: :error

  defp target(path) do
    case folder(path) do
      {:ok, [_ | _] = segments} -> {:ok, Enum.drop(segments, -1), List.last(segments)}
      _root_or_refused -> :error
    end
  end

  # An unfolded level keeps the levels above it, so none hides under a folded parent. They are as live as the rest.
  defp open(socket, segments, raw) do
    keep = for path <- [segments | socket.assigns.expanded], n <- 0..length(path), uniq: true, do: Enum.take(path, n)

    socket
    |> cancel_uploads()
    |> assign(tree: Map.take(socket.assigns.tree, keep))
    |> assign(segments: segments, page_title: "/" <> Enum.join(segments, "/"), crumbs: crumbs(segments))
    |> assign(entries_hash: nil, filter: "", filter_form: to_form(%{"filter" => ""}), selected: nil, confirm: nil)
    |> assign(picker: %{}, target: nil, moving: nil)
    |> put_raw(raw)
  end

  defp cancel_uploads(%{assigns: %{uploads: %{files: files}}} = socket),
    do: Enum.reduce(files.entries, socket, &cancel_upload(&2, :files, &1.ref))

  defp cancel_uploads(socket), do: socket

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
    <Layouts.app flash={@flash} write?={@write?} rail={@rail}>
      <:sidebar>
        <div class="group fades">
          <span class="head">Folders</span>
          <div id="tree">
            <Tree.nodes tree={@tree} current={@segments} drop?={@write?} />
          </div>
        </div>
        <section id="card" class="card cats fades" aria-label="Files in this folder by kind">
          <div class="title">
            <h2>In this folder</h2>
            <span class="meta">{@card.meta}</span>
          </div>
          <div class="body">
            <svg
              :if={@card.kinds != []}
              class="bar"
              viewBox="0 0 100 6"
              preserveAspectRatio="none"
              aria-hidden="true"
            >
              <rect
                :for={k <- @card.kinds}
                class={"k-" <> Atom.to_string(k.kind)}
                x={k.x}
                width={k.w}
                height="6"
                rx="1"
              />
            </svg>
            <div :for={k <- @card.kinds} class="cat" data-kind={k.kind}>
              <span class={["swatch", "k-" <> Atom.to_string(k.kind)]}></span>
              <span>{Kind.plural(k.kind)}</span>
              <i class="n">{k.count}</i>
              <b class="size">{k.size}</b>
            </div>
            <p :if={@card.kinds == []} class="sub">No files here.</p>
          </div>
        </section>
      </:sidebar>

      <%!-- data-path is the folder this process lists; the hook compares it to the URL after a reconnect. --%>
      <div id="listing" data-view={@view} data-path={href(@segments)} phx-hook=".Listing">
        <header class="ph">
          <h1 class="sr-only">{List.last(@segments) || "Files"}</h1>
          <nav id="crumbs" class="crumbs" aria-label="Folder path">
            <a href={~p"/"} data-open aria-current={@segments == [] && "page"}>
              <.icon name="hero-home" class="size-3" /> Root
            </a>
            <span :for={{name, path} <- @crumbs} class="crumb">
              <.icon name="hero-chevron-right" class="size-3" />
              <a href={path} data-open aria-current={path == href(@segments) && "page"}>{name}</a>
            </span>
          </nav>
          <span class="meta">{@meta}</span>
          <div class="tools">
            <.form
              for={@filter_form}
              id="filter-form"
              class="filter"
              phx-change="filter"
              phx-submit="filter"
            >
              <.icon name="hero-magnifying-glass" class="size-4" />
              <.input
                field={@filter_form[:filter]}
                type="search"
                class="filter-input"
                placeholder="Filter"
                aria-label="Filter this folder"
                autocomplete="off"
                phx-debounce="100"
              />
              <kbd>/</kbd>
            </.form>
            <section :if={@write?} id="write-bar" class="write-bar">
              <.form for={@upload_form} id="upload-form" phx-change="validate" phx-submit="upload">
                <.input type="hidden" name="path" value={href(@segments)} />
                <label class="btn">
                  <.icon name="hero-arrow-up-tray" class="size-4" /> Upload
                  <.live_file_input upload={@uploads.files} class="sr-only" />
                </label>
              </.form>
              <button type="button" class="btn" popovertarget="mkdir-pop">
                <.icon name="hero-plus" class="size-4" /> New folder
              </button>
              <div id="mkdir-pop" popover class="surface pop anchored">
                <.form for={@mkdir_form} id="mkdir-form" class="mkdir" phx-submit="mkdir">
                  <.input type="hidden" name="path" value={href(@segments)} />
                  <.input
                    field={@mkdir_form[:name]}
                    placeholder="Folder name"
                    aria-label="New folder name"
                    required
                  />
                  <button type="submit" class="btn primary">Create</button>
                </.form>
              </div>
            </section>
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
        </header>

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
          <li :if={@uploads.files.entries != []} class="upload-go">
            <button type="submit" form="upload-form" class="btn primary" id="upload-submit">
              <.icon name="hero-arrow-up-tray" class="size-4" /> Upload
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
              phx-value-path={@confirm_path}
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

        <div class="split" phx-drop-target={@write? && @uploads.files.ref}>
          <div class="list-wrap">
            <table class="table listing" inert={not is_nil(@confirm)}>
              <thead>
                <tr>
                  <th>Name</th>
                  <th class="kind-col">Kind</th>
                  <th class="num">Size</th>
                  <th class="num when">Modified</th>
                </tr>
              </thead>
              <tbody id="entries" phx-update="stream">
                <tr id="entries-empty" class="empty">
                  <td colspan="4">
                    <.icon name="hero-folder-open" class="size-6" />
                    <strong>Nothing to show here.</strong>
                  </td>
                </tr>
                <tr
                  :for={{id, entry} <- @streams.entries}
                  id={id}
                  data-name={entry.name}
                  aria-selected={to_string(entry.selected?)}
                  draggable={@write? && "true"}
                  phx-click={JS.push("select", value: %{name: entry.name})}
                >
                  <td class="name">
                    <a href={entry.href} data-open={entry.dir?}>
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
                  <td class="kind-col">
                    <span class={["kindtag", "k-" <> Atom.to_string(entry.kind)]}>
                      <span :if={!entry.dir?} class="swatch"></span>{Kind.label(entry.kind)}
                    </span>
                  </td>
                  <td class="num size">{entry.size}</td>
                  <td class="num when">
                    <time :if={entry.mtime} datetime={iso(entry.mtime)} title={utc(entry.mtime)}>
                      {ago(entry.mtime)}
                    </time>
                    <%!-- The empty command keeps the row's select from firing: the menu opens alone. --%>
                    <button
                      type="button"
                      class="rowbtn"
                      phx-click={%JS{}}
                      popovertarget={"menu-" <> id}
                      aria-label={"Actions for " <> entry.name}
                    >
                      <.icon name="hero-ellipsis-horizontal" class="size-4" />
                    </button>
                    <div id={"menu-" <> id} popover class="surface menu anchored" phx-click={%JS{}}>
                      <button :if={@write?} type="button" data-act="rename" data-name={entry.name}>
                        <.icon name="hero-pencil" class="size-4" /> Rename <kbd>F2</kbd>
                      </button>
                      <button :if={@write?} type="button" data-act="move" data-name={entry.name}>
                        <.icon name="hero-folder-arrow-down" class="size-4" /> Move to… <kbd>M</kbd>
                      </button>
                      <a :if={!entry.dir?} href={entry.href} download>
                        <.icon name="hero-arrow-down-tray" class="size-4" /> Download
                      </a>
                      <button type="button" data-act="copy" data-href={entry.href}>
                        <.icon name="hero-link" class="size-4" /> Copy link
                      </button>
                      <hr :if={@write?} />
                      <button
                        :if={@write?}
                        type="button"
                        class="danger"
                        phx-click="delete"
                        phx-value-path={entry.href}
                      >
                        <.icon name="hero-trash" class="size-4" /> Delete <kbd>Del</kbd>
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <aside
            :if={@selected}
            id="inspector"
            class="inspector"
            data-name={@selected.name}
            aria-label="Selected entry"
            phx-remove={JS.hide(transition: "out", time: 200)}
          >
            <button type="button" class="icon-btn close" phx-click="deselect" aria-label="Close">
              <.icon name="hero-x-mark" class="size-4" />
            </button>
            <div class="preview">
              <%= cond do %>
                <% @selected.thumb -> %>
                  <%!-- This image is often the request that makes the thumbnail and stores its size. --%>
                  <img src={@selected.thumb} alt="" data-redim={!@selected.dims && "true"} />
                <% @selected.ext == "SVG" -> %>
                  <img src={@selected.href} alt="" />
                <% @selected.kind == :video -> %>
                  <video src={@selected.href} preload="metadata" controls></video>
                <% @selected.kind == :audio -> %>
                  <audio src={@selected.href} preload="metadata" controls></audio>
                <% true -> %>
                  <.icon name={kind_icon(@selected.kind)} class="size-8" />
              <% end %>
            </div>
            <.form :if={@write?} for={@rename_form} id="rename-form" phx-submit="rename">
              <.input type="hidden" name="path" value={@selected.href} />
              <.input
                field={@rename_form[:name]}
                id="rename-name"
                label="Name"
                autocomplete="off"
                required
              />
              <p class="hint">Enter renames · Esc cancels</p>
            </.form>
            <h2 :if={!@write?} class="iname">{@selected.name}</h2>
            <p class="kindline">{kind_line(@selected)}</p>
            <table class="kv">
              <tr>
                <td>Size</td>
                <td>{@selected.size}</td>
              </tr>
              <tr :if={@selected.mtime}>
                <td>Modified</td>
                <td>{utc(@selected.mtime)}</td>
              </tr>
              <tr>
                <td>Folder</td>
                <td>{List.last(@segments) || "Root"}</td>
              </tr>
            </table>
            <div class="acts">
              <a
                :if={!@selected.dir?}
                class="btn primary"
                href={@selected.href}
                download
                id="inspector-download"
              >
                <.icon name="hero-arrow-down-tray" class="size-4" /> Download
              </a>
              <a :if={@selected.dir?} class="btn primary" href={@selected.href} data-open>
                <.icon name="hero-folder-open" class="size-4" /> Open
              </a>
              <button type="button" class="btn" data-act="copy" data-href={@selected.href}>
                <.icon name="hero-link" class="size-4" /> Copy link
              </button>
            </div>
            <div :if={@write?} class="acts sep">
              <button
                type="button"
                class="btn"
                id="move-open"
                phx-click={JS.push("move_open", value: %{path: @selected.href})}
              >
                <.icon name="hero-folder-arrow-down" class="size-4" /> Move to…
              </button>
              <button
                type="button"
                class="btn danger"
                id="inspector-delete"
                phx-click={JS.push("delete", value: %{path: @selected.href})}
              >
                <.icon name="hero-trash" class="size-4" /> Delete
              </button>
            </div>
          </aside>
        </div>

        <dialog
          :if={@write? and @moving}
          id="move-dialog"
          class="surface dialog"
          aria-labelledby="move-title"
          phx-mounted={JS.ignore_attributes("open")}
        >
          <div class="dh">
            <h2 id="move-title">Move “{@moving}”</h2>
            <p class="hint">Pick a destination folder</p>
          </div>
          <div id="picker" class="picker">
            <div class={["node", @target == [] && "on"]} data-path="" aria-expanded="true">
              <span class="chev"><.icon name="hero-chevron-down" class="size-3" /></span>
              <button
                type="button"
                class="pick"
                phx-click="move_pick"
                phx-value-path=""
                aria-pressed={to_string(@target == [])}
              >
                <.icon name="hero-folder-open" class="size-4 fi" />
                <span class="nm">Root</span>
                <span class="ct">{if(@moving_in == [], do: "current")}</span>
              </button>
            </div>
            <Tree.nodes tree={@picker} current={@moving_in} pick={@target} />
          </div>
          <.form for={@move_form} id="move-form" data-name={@moving} phx-submit="rename">
            <.input type="hidden" name="path" value={href(@moving_in ++ [@moving])} />
            <.input
              type="hidden"
              name="to"
              value={@target && Enum.join(@target ++ [@moving], "/")}
            />
            <button type="button" class="btn" data-act="close">Cancel</button>
            <button type="submit" class="btn primary" disabled={@target in [nil, @moving_in]}>
              Move here
            </button>
          </.form>
        </dialog>

        <div id="drop-hint" class="toast" phx-update="ignore" hidden></div>
        <div id="copy-box" class="toast" phx-update="ignore" hidden>
          <input readonly aria-label="Link to copy" />
        </div>
      </div>
      <script :type={Phoenix.LiveView.ColocatedHook} name=".Listing">
        // LiveViewTest runs none of this: the browser probe and the owner's preview cover it.
        const typing = t => t.closest?.("input, textarea, select, [contenteditable]")
        const $ = id => document.getElementById(id)
        // A write names its entry from the folder shown when the user acted, so one queued behind an open keeps it.
        const here = name => `${$("listing").dataset.path.replace(/\/$/, "")}/${encodeURIComponent(name)}`
        const decode = p => { try { return decodeURIComponent(p) } catch { return p } }
        const ours = e => e.state?.slimserve === true || decode(location.pathname) !== decode($("listing")?.dataset.path ?? "")
        // Added when the bundle loads, before LiveSocket.connect adds its own. Listeners on window run in the
        // order they were added, so this one stops LiveView's, which would load the URL as a new LiveView.
        let onPop = null
        window.addEventListener("popstate", e => {
          if (!onPop || !ours(e)) return
          e.stopImmediatePropagation()
          onPop()
        })

        export default {
          mounted() {
            const hint = $("drop-hint")
            let hintTimer = null
            const say = (text, ms) => {
              hint.textContent = text
              hint.hidden = false
              clearTimeout(hintTimer)
              if (ms) hintTimer = setTimeout(() => { hint.hidden = true }, ms)
            }
            this.handleEvent("move_ready", () => { const d = $("move-dialog"); if (d && !d.open) d.showModal() })
            const move = () => $("move-open")?.click()
            const focusRename = () => { const f = $("rename-name"); if (f) { f.focus(); f.select() } }
            // Rename lives in the inspector.
            const act = (kind, name) =>
              (kind === "rename" ? this.pushEvent("select", {name}).then(focusRename) : this.pushEvent("move_open", {path: here(name)}))
                .catch(() => {})

            // Through js() so the open's own patch keeps it.
            const open = path => {
              this.js().addClass(this.el, "opening")
              return this.pushEvent("open", {path}).then(r => r?.ok === true, () => false)
                .finally(() => this.js().removeClass(this.el, "opening"))
            }
            this.onClick = e => {
              const a = e.target.closest?.("a[data-open]")
              if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
              e.preventDefault()
              // Before LiveView's window listener, so the row's own select never fires.
              e.stopPropagation()
              if (a.href === location.href) return
              open(new URL(a.href).pathname).then(ok => ok ? history.pushState({slimserve: true}, "", a.href) : location.assign(a.href))
            }
            document.addEventListener("click", this.onClick)
            onPop = () => open(location.pathname).then(ok => ok || location.reload())

            const copy = href => {
              const url = new URL(href, window.location.origin).href
              const fallback = () => {
                const box = $("copy-box"), input = box.querySelector("input")
                box.hidden = false
                input.value = url
                input.focus()
                input.select()
                input.onblur = () => { box.hidden = true }
              }
              // No clipboard outside a secure context.
              if (!navigator.clipboard) return fallback()
              navigator.clipboard.writeText(url).then(() => say("Link copied", 1500), fallback)
            }

            this.el.addEventListener("click", e => {
              const b = e.target.closest("[data-act]")
              if (!b) return
              b.closest("[popover]")?.hidePopover()
              const kind = b.dataset.act
              if (kind === "copy") copy(b.dataset.href)
              else if (kind === "close") b.closest("dialog")?.close()
              else if (b.dataset.name) act(kind, b.dataset.name)
            })
            // The inspector's thumbnail has loaded, so its size is cached now: read it again.
            this.el.addEventListener("load", e => {
              if (e.target.matches?.("#inspector img[data-redim]")) this.pushEvent("select", {name: e.target.closest("#inspector").dataset.name}).catch(() => {})
            }, true)
            this.el.addEventListener("submit", e => {
              if (e.target.id === "move-form") $("move-dialog")?.close()
              e.target.closest("[popover]")?.hidePopover()
            })

            let at = null, held = null
            this.el.addEventListener("contextmenu", e => {
              const pop = e.target.closest?.("tr[data-name]")?.querySelector(".menu")
              if (!pop || e.target.closest("[popover]")) return
              e.preventDefault()
              if (pop.matches(":popover-open")) pop.hidePopover()
              at = {x: e.clientX, y: e.clientY}
              // On Linux it fires while the button is down, and Gecko light-dismisses a popover opened then on its release.
              held = e.buttons & 2 ? pop : null
              if (!held) pop.showPopover()
            })
            // A held menu belongs to one press: a release outside the window never comes.
            this.onDown = () => { held = at = null }
            this.onUp = e => {
              const pop = e.button === 2 ? held : null
              held = null
              if (pop?.isConnected) pop.showPopover()
            }
            document.addEventListener("pointerdown", this.onDown)
            document.addEventListener("pointerup", this.onUp)

            // The patch must not drop the position, so the style attribute is the client's.
            this.onToggle = e => {
              const pop = e.target
              if (e.newState !== "open" || !pop.matches?.("[popover].anchored")) return
              const trigger = document.querySelector(`[popovertarget="${pop.id}"]`)
              const point = at, w = pop.offsetWidth, h = pop.offsetHeight
              at = null
              const r = point ? {top: point.y, bottom: point.y} : trigger?.getBoundingClientRect()
              if (!r) return
              const gap = point ? 2 : 6
              const left = point ? point.x : r.right - w
              this.js().ignoreAttributes(pop, "style")
              pop.style.top = `${r.bottom + gap + h > window.innerHeight ? Math.max(8, r.top - h - gap) : r.bottom + gap}px`
              pop.style.left = `${Math.max(8, Math.min(left, window.innerWidth - w - 8))}px`
            }
            document.addEventListener("toggle", this.onToggle, true)

            let dragged = null, over = null
            const hover = node => {
              if (over === node) return
              over?.classList.remove("drop")
              over = node
              if (!node) return say("", 1)
              node.classList.add("drop")
              say(`Drop on ${node.querySelector(".nm").textContent} to move 1 ${dragged.dir ? "folder" : "file"}`)
            }
            this.el.addEventListener("dragstart", e => {
              const row = e.target.closest?.("tr[data-name]")
              if (!row) return
              dragged = {name: row.dataset.name, path: here(row.dataset.name), dir: !!row.querySelector(".kind.dir")}
              e.dataTransfer.setData("text/plain", dragged.name)
              e.dataTransfer.effectAllowed = "move"
            })
            this.el.addEventListener("dragend", () => { hover(null); dragged = null })
            this.onOver = e => {
              const node = dragged && e.target.closest?.("[data-drop]")
              hover(node || null)
              if (!node) return
              e.preventDefault()
              e.dataTransfer.dropEffect = "move"
            }
            this.onDrop = e => {
              const node = dragged && e.target.closest?.("[data-drop]")
              if (!node) return
              e.preventDefault()
              this.pushEvent("rename", {path: dragged.path, to: `${node.dataset.drop}/${dragged.name}`}).catch(() => {})
              hover(null)
            }
            document.addEventListener("dragover", this.onOver)
            document.addEventListener("drop", this.onDrop)

            // Del deletes a file with no confirm.
            this.onKey = e => {
              if (e.ctrlKey || e.metaKey || e.altKey) return
              const t = e.target
              if (t.id === "rename-name" && e.key === "Escape") {
                t.value = t.defaultValue
                t.blur()
                return
              }
              if (typing(t) || document.querySelector("dialog[open]")) return
              const menu = document.querySelector(".menu:popover-open")
              const row = menu?.closest("tr[data-name]")
              // A closed inspector stays for its exit animation.
              const inspector = document.querySelector("#inspector:not(.out)")
              const name = row ? row.dataset.name : inspector?.dataset.name
              const can = (inMenu, inInspector) => row ? !!menu.querySelector(inMenu) : !!inspector?.querySelector(inInspector)
              if (e.key === "/") {
                e.preventDefault()
                $("filter-form")?.querySelector("input")?.focus()
              } else if (name && e.key === "F2" && can("[data-act=rename]", "#rename-name")) {
                e.preventDefault()
                menu?.hidePopover()
                row ? act("rename", name) : focusRename()
              } else if (name && (e.key === "m" || e.key === "M") && can("[data-act=move]", "#move-open")) {
                e.preventDefault()
                menu?.hidePopover()
                row ? act("move", name) : move()
              } else if (name && e.key === "Delete" && can("button[phx-click=delete]", "#inspector-delete")) {
                e.preventDefault()
                menu?.hidePopover()
                this.pushEvent("delete", {path: here(name)}).catch(() => {})
              }
            }
            window.addEventListener("keydown", this.onKey)
            this.sync()
          },
          // A reconnect mounts the session's folder, which an in-page open may have left.
          reconnected() { this.sync() },
          sync() {
            if (decode(this.el.dataset.path) === decode(location.pathname)) return
            this.pushEvent("open", {path: location.pathname}).then(r => r?.ok === true || location.reload(), () => {})
          },
          destroyed() {
            document.removeEventListener("click", this.onClick)
            onPop = null
            document.removeEventListener("toggle", this.onToggle, true)
            document.removeEventListener("pointerdown", this.onDown)
            document.removeEventListener("pointerup", this.onUp)
            document.removeEventListener("dragover", this.onOver)
            document.removeEventListener("drop", this.onDrop)
            window.removeEventListener("keydown", this.onKey)
          }
        }
      </script>
    </Layouts.app>
    """
  end

  defp row(socket, entry) do
    %{segments: segments, counts: counts, selected: selected} = socket.assigns
    path = segments ++ [entry.name]

    %{
      # The row's DOM id: from the name, so a relist never patches a row with another entry.
      id: Base.url_encode64(entry.name, padding: false),
      name: entry.name,
      dir?: entry.dir?,
      kind: Kind.of(entry.name, entry.dir?),
      href: href(path),
      thumb: entry.thumb && href(path) <> "?thumb=1&v=" <> URI.encode_www_form(entry.thumb),
      size: if(entry.dir?, do: items(counts[entry.name]), else: human(entry.size)),
      # An S3 folder is a key prefix, which has no time of its own.
      mtime: entry.mtime,
      selected?: selected != nil and selected.name == entry.name
    }
  end

  defp inspected(socket, entry) do
    row = row(socket, entry)
    ext = entry.name |> Path.extname() |> String.trim_leading(".") |> String.upcase()
    Map.merge(row, %{ext: ext, dims: entry.thumb && dims(socket.assigns.segments ++ [entry.name], entry.thumb)})
  end

  defp dims(path, version) do
    max_dim = Application.fetch_env!(:slimserve, :thumb_max_dim)

    key =
      if Storage.s3?() do
        ThumbCache.key(Enum.join(path, "/"), version, max_dim)
      else
        with {:file, file} when is_binary(file) <- Storage.resolve(path),
             {:ok, stat} <- File.stat(file, time: :posix),
             do: ThumbCache.key(file, stat, max_dim),
             else: (_ -> nil)
      end

    key && ThumbCache.size(key)
  end

  defp kind_line(%{kind: :folder, size: size}), do: Enum.join(["Folder", size], " · ")
  defp kind_line(%{ext: "", kind: kind}), do: Kind.label(kind)

  defp kind_line(%{ext: ext, kind: kind, dims: dims}) do
    line = ext <> " " <> String.downcase(Kind.label(kind))
    if dims, do: "#{line} · #{elem(dims, 0)} × #{elem(dims, 1)}", else: line
  end

  defp kind_icon(:folder), do: "hero-folder"
  defp kind_icon(:image), do: "hero-photo"
  defp kind_icon(:document), do: "hero-document-text"
  defp kind_icon(:archive), do: "hero-archive-box"
  defp kind_icon(:code), do: "hero-code-bracket"
  defp kind_icon(:other), do: "hero-document"

  defp meta(raw) do
    bytes = for %{dir?: false, size: size} <- raw, reduce: 0, do: (sum -> sum + size)
    "#{items(length(raw))} · #{human(bytes)}"
  end

  defp card(raw) do
    files = for %{dir?: false} = entry <- raw, do: {Kind.of(entry.name, false), entry.size}
    groups = Enum.group_by(files, &elem(&1, 0), &elem(&1, 1))
    total = files |> Enum.map(&elem(&1, 1)) |> Enum.sum()

    {kinds, _x} =
      for kind <- Kind.order(), sizes = Map.get(groups, kind), sizes != nil, reduce: {[], 0} do
        {acc, x} ->
          bytes = Enum.sum(sizes)
          w = if total > 0, do: bytes * 100 / total, else: length(sizes) * 100 / length(files)
          {acc ++ [%{kind: kind, count: length(sizes), size: human(bytes), x: x, w: w}], x + w}
      end

    file_count = length(files)
    %{kinds: kinds, meta: "#{file_count} #{if file_count == 1, do: "file", else: "files"} · #{human(total)}"}
  end

  defp items(nil), do: ""
  defp items(1), do: "1 item"
  defp items(count), do: "#{count} items"

  defp iso(mtime), do: mtime |> DateTime.from_unix!() |> DateTime.to_iso8601()
  defp utc(mtime), do: mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d %H:%M UTC")

  # Rendered with the row, so it goes stale until the row renders again.
  defp ago(mtime) do
    seconds = System.os_time(:second) - mtime

    cond do
      seconds < 0 -> mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d")
      seconds < 60 -> "just now"
      seconds < 3600 -> "#{div(seconds, 60)}m ago"
      seconds < 86_400 -> "#{div(seconds, 3600)}h ago"
      seconds < 2 * 86_400 -> "yesterday"
      seconds < 7 * 86_400 -> "#{div(seconds, 86_400)} days ago"
      seconds < 14 * 86_400 -> "last week"
      true -> mtime |> DateTime.from_unix!() |> Calendar.strftime("%Y-%m-%d")
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
