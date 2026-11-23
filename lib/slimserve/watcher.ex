defmodule SlimServe.Watcher do
  @moduledoc """
  Watches the root with inotify and broadcasts `{:dir_changed, rel_dir}` on `SlimServe.Root.topic(rel_dir)`
  for the directory each event happened in.

  A bare modify is dropped, so a long write wakes listings once it closes. Other events are not filtered:
  a dot-file or an ignored file still wakes its directory's listing, which re-lists through `SlimServe.Root`
  and so never shows it.
  """
  use GenServer, restart: :temporary

  alias SlimServe.Root

  require Logger

  def start_link(opts), do: GenServer.start_link(__MODULE__, Keyword.fetch!(opts, :root))

  @impl true
  def init(root) do
    # FileSystem answers :ignore without inotifywait, so this match fails the boot.
    {:ok, pid} = FileSystem.start_link(dirs: [root])
    :ok = FileSystem.subscribe(pid)
    {:ok, root}
  end

  @impl true
  # file_system reports both MODIFY and CLOSE_WRITE as :modified; inotifywait adds CLOSE to the latter.
  def handle_info({:file_event, _pid, {_path, [:modified]}}, root), do: {:noreply, root}

  def handle_info({:file_event, _pid, {path, events}}, root) do
    # An event on a directory also goes to the directory's own topic: file_system asks inotifywait for no
    # DELETE_SELF, so an empty directory that is removed or renamed sends nothing from inside.
    dirs = if :isdir in events, do: [Path.dirname(path), path], else: [Path.dirname(path)]

    for dir <- dirs, rel_dir = Path.relative_to(dir, root) do
      Phoenix.PubSub.broadcast(SlimServe.PubSub, Root.topic(rel_dir), {:dir_changed, rel_dir})
    end

    {:noreply, root}
  end

  # inotifywait exited, for example past fs.inotify.max_user_watches. Not restarted: file serving stays up.
  # :shutdown, not :normal, so the linked FileSystem worker stops too.
  def handle_info({:file_event, _pid, :stop}, root) do
    Logger.error("inotifywait stopped watching #{root}: live listing updates are off until restart")
    {:stop, :shutdown, root}
  end
end
