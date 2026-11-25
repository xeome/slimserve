defmodule SlimServe.DiskCache do
  @moduledoc """
  Thumbnails (`SlimServe.ThumbCache`) and S3 downloads each run their own cache, with its own directory and cap.

  The directory is the index, so a hit is the file existing and a restart loses nothing. This process
  is the only one that writes or deletes there, and it touches only names shaped like its own keys.

  A held file is never evicted, and held room counts against the cap, until `release/2` or until the
  holder exits. So a file being sent is not deleted under the send.
  """
  use GenServer

  require Logger

  @type key :: String.t()

  def child_spec(opts), do: %{id: Keyword.fetch!(opts, :name), start: {__MODULE__, :start_link, [opts]}}

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

  @doc "The cached bytes, read in the caller. One that is evicted before it is read is a miss."
  @spec read(atom(), key()) :: {:ok, binary()} | :miss
  # The path is the configured cache directory plus a key, and keys are SHA-256 hex digests.
  # sobelow_skip ["Traversal.FileModule"]
  def read(name, key) do
    with {dir, ext} <- :persistent_term.get({__MODULE__, name}, nil),
         path = path(dir, key, ext),
         {:ok, bytes} <- File.read(path) do
      touch(path)
      {:ok, bytes}
    else
      _ -> :miss
    end
  end

  @spec put(atom(), key(), binary()) :: :ok
  def put(name, key, bytes), do: GenServer.cast(name, {:put, key, bytes})

  @spec claim(atom(), key()) :: {:ok, Path.t()} | :miss
  def claim(name, key), do: GenServer.call(name, {:claim, key, self()})

  @spec reserve(atom(), non_neg_integer()) :: {:ok, Path.t()} | :full
  def reserve(name, bytes), do: GenServer.call(name, {:reserve, bytes, self()})

  @spec publish(atom(), Path.t(), key()) :: {:ok, Path.t()} | :error
  def publish(name, tmp, key), do: GenServer.call(name, {:publish, tmp, key, self()})

  @spec release(atom(), Path.t()) :: :ok
  def release(name, path), do: GenServer.call(name, {:release, path, self()})

  @impl true
  # The paths are the configured cache directory and the key-shaped .tmp names listed in it.
  # sobelow_skip ["Traversal.FileModule"]
  def init(opts) do
    dir = Keyword.fetch!(opts, :dir)
    ext = Keyword.fetch!(opts, :ext)
    state = %{dir: nil, ext: ext, max_bytes: Keyword.fetch!(opts, :max_bytes), bytes: 0, holds: []}

    state =
      with :ok <- File.mkdir_p(dir),
           {:ok, names} <- File.ls(dir) do
        # A .tmp file is a write that a crash cut short.
        for name <- names, name =~ ~r/^[0-9a-f]{64}\.tmp$/, do: File.rm(Path.join(dir, name))
        %{state | dir: dir, bytes: state |> Map.put(:dir, dir) |> files(names) |> total()}
      else
        {:error, reason} ->
          Logger.warning("running without a #{ext} cache: #{dir}: #{inspect(reason)}")
          state
      end

    # Where read/2 finds the directory without asking this process. nil: no cache.
    :persistent_term.put({__MODULE__, Keyword.fetch!(opts, :name)}, state.dir && {state.dir, ext})
    {:ok, state}
  end

  @impl true
  def handle_cast({:put, _key, _bytes}, %{dir: nil} = state), do: {:noreply, state}

  # The paths are the configured cache directory plus a key, and keys are SHA-256 hex digests.
  # sobelow_skip ["Traversal.FileModule"]
  def handle_cast({:put, key, bytes}, %{dir: dir} = state) do
    path = path(dir, key, state.ext)
    tmp = Path.join(dir, key <> ".tmp")

    # The rename is atomic, so a reader sees the whole file or none. mkdir_p: a /tmp cleaner may
    # have removed an idle cache directory.
    with false <- File.exists?(path),
         :ok <- File.mkdir_p(dir),
         :ok <- File.write(tmp, bytes),
         :ok <- File.rename(tmp, path) do
      {:noreply, evict(%{state | bytes: state.bytes + byte_size(bytes)}, 0, [path])}
    else
      true ->
        {:noreply, state}

      {:error, reason} ->
        Logger.warning("#{state.ext} cache write failed: #{path}: #{inspect(reason)}")
        {:noreply, state}
    end
  end

  @impl true
  def handle_call({:claim, _key, _pid}, _from, %{dir: nil} = state), do: {:reply, :miss, state}
  def handle_call({:reserve, _bytes, _pid}, _from, %{dir: nil} = state), do: {:reply, :full, state}

  def handle_call({:claim, key, pid}, _from, state) do
    path = path(state.dir, key, state.ext)

    if File.exists?(path) do
      touch(path)
      {:reply, {:ok, path}, hold(state, pid, path, 0)}
    else
      {:reply, :miss, state}
    end
  end

  # sobelow_skip ["Traversal.FileModule"]
  def handle_call({:reserve, bytes, pid}, _from, state) do
    made? = File.mkdir_p(state.dir) == :ok
    state = if bytes <= state.max_bytes, do: evict(state, bytes, []), else: state

    if made? and state.bytes + reserved(state) + bytes <= state.max_bytes do
      tmp = Path.join(state.dir, Base.encode16(:crypto.strong_rand_bytes(32), case: :lower) <> ".tmp")
      {:reply, {:ok, tmp}, hold(state, pid, tmp, bytes)}
    else
      {:reply, :full, state}
    end
  end

  # sobelow_skip ["Traversal.FileModule"]
  def handle_call({:publish, tmp, key, pid}, _from, state) do
    path = path(state.dir, key, state.ext)

    # Two misses of one object can both publish it: the second replaces the first, counted once.
    replaced =
      case File.stat(path) do
        {:ok, %File.Stat{size: size}} -> size
        _ -> 0
      end

    with {:ok, {_ref, ^pid, ^tmp, _bytes} = held} <- held(state, pid, tmp),
         {:ok, %File.Stat{size: size}} <- File.stat(tmp),
         :ok <- File.rename(tmp, path) do
      state = %{unhold(state, held) | bytes: state.bytes + size - replaced}
      {:reply, {:ok, path}, hold(state, pid, path, 0)}
    else
      _ -> {:reply, :error, state}
    end
  end

  def handle_call({:release, path, pid}, _from, state) do
    case held(state, pid, path) do
      {:ok, held} -> {:reply, :ok, drop(state, held)}
      :error -> {:reply, :ok, state}
    end
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, _reason}, state) do
    {:noreply, state.holds |> Enum.filter(&(elem(&1, 0) == ref)) |> Enum.reduce(state, &drop(&2, &1))}
  end

  defp hold(state, pid, path, bytes), do: %{state | holds: [{Process.monitor(pid), pid, path, bytes} | state.holds]}

  defp held(state, pid, path) do
    case Enum.find(state.holds, &match?({_ref, ^pid, ^path, _bytes}, &1)) do
      nil -> :error
      held -> {:ok, held}
    end
  end

  defp unhold(state, {ref, _pid, _path, _bytes} = held) do
    Process.demonitor(ref, [:flush])
    %{state | holds: List.delete(state.holds, held)}
  end

  # A reservation still held is a private temporary file that never became an entry.
  # sobelow_skip ["Traversal.FileModule"]
  defp drop(state, {_ref, _pid, path, bytes} = held) do
    if bytes > 0 or String.ends_with?(path, ".tmp"), do: File.rm(path)
    unhold(state, held)
  end

  defp reserved(state), do: state.holds |> Enum.map(&elem(&1, 3)) |> Enum.sum()

  defp path(dir, key, ext), do: Path.join(dir, key <> "." <> ext)

  # Not File.touch/1, which would make the file again when an eviction got there first.
  defp touch(path), do: :file.change_time(path, :erlang.localtime())

  defp evict(state, incoming, keep) do
    if state.bytes + reserved(state) + incoming <= state.max_bytes, do: state, else: shrink(state, incoming, keep)
  end

  # Recounts from the directory, because the running total goes stale when files are deleted from
  # outside. File times have one-second resolution, so the file just put can sort before older ones.
  # The paths are the key-shaped names listed in the configured cache directory.
  # sobelow_skip ["Traversal.FileModule"]
  defp shrink(state, incoming, keep) do
    # A directory removed from outside holds nothing.
    names =
      case File.ls(state.dir) do
        {:ok, names} -> names
        {:error, _reason} -> []
      end

    files = state |> files(names) |> Enum.sort()
    held = MapSet.new(state.holds, &elem(&1, 2))
    target = div(state.max_bytes * 9, 10) - reserved(state) - incoming

    bytes =
      Enum.reduce(files, total(files), fn
        {_mtime, path, size}, bytes when bytes > target ->
          if path in keep or MapSet.member?(held, path) do
            bytes
          else
            _ = File.rm(path)
            bytes - size
          end

        _file, bytes ->
          bytes
      end)

    %{state | bytes: bytes}
  end

  # Anything else in the directory is not this cache's to count or delete.
  defp files(%{dir: dir, ext: ext}, names) do
    for name <- names,
        name =~ ~r/^[0-9a-f]{64}\./ and String.ends_with?(name, "." <> ext) and byte_size(name) == 65 + byte_size(ext),
        path = Path.join(dir, name),
        {:ok, stat} <- [File.stat(path, time: :posix)],
        do: {stat.mtime, path, stat.size}
  end

  defp total(files), do: files |> Enum.map(&elem(&1, 2)) |> Enum.sum()
end
