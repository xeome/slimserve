defmodule SlimServe.ThumbCache do
  @moduledoc """
  The thumbnail disk cache: one `<key>.webp` file per thumbnail in one directory, kept under a byte cap.

  The directory is the index, so a hit is the file existing and a restart loses nothing. A hit is read
  by the caller and sends this process nothing, so a busy or restarting cache still serves. This process
  is the only one that writes or deletes there, and it touches only names shaped like its own keys: two
  requests that miss the same image cannot interleave their writes. libvips runs in the caller.

  A directory that cannot be made or listed at start leaves the server without a cache: every lookup
  misses and every put is dropped.
  """
  use GenServer

  require Logger

  @type key :: String.t()

  @doc "Starts the cache on `:dir` with the cap `:max_bytes`, registered as `:name` (this module by default)."
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: name(opts))

  @doc """
  The key of one source file at one thumbnail size: 64 hex digits. It changes when the file's path,
  inode, times or size do, so a replacement that keeps the size and the modification time still misses.
  """
  @spec key(Path.t(), File.Stat.t(), pos_integer()) :: key()
  def key(path, %File.Stat{inode: inode, ctime: ctime, mtime: mtime, size: size}, max_dim) do
    term = :erlang.term_to_binary({path, inode, ctime, mtime, size, max_dim})
    :sha256 |> :crypto.hash(term) |> Base.encode16(case: :lower)
  end

  @doc "The cached thumbnail's bytes, read in the caller. One that is evicted before it is read is a miss."
  @spec lookup(atom(), key()) :: {:ok, binary()} | :miss
  # The path is the configured cache directory plus a key, and key/3 makes that from a SHA-256.
  # sobelow_skip ["Traversal.FileModule"]
  def lookup(name \\ __MODULE__, key) do
    with dir when is_binary(dir) <- :persistent_term.get({__MODULE__, name}, nil),
         {:ok, webp} <- File.read(path(dir, key)) do
      {:ok, webp}
    else
      _ -> :miss
    end
  end

  @doc "Stores a thumbnail, then deletes the oldest ones while the cache is over its cap. Does not wait for either."
  @spec put(atom(), key(), binary()) :: :ok
  def put(name \\ __MODULE__, key, webp), do: GenServer.cast(name, {:put, key, webp})

  @impl true
  # The paths are the configured cache directory and the key-shaped .tmp names listed in it.
  # sobelow_skip ["Traversal.FileModule"]
  def init(opts) do
    dir = Keyword.fetch!(opts, :dir)
    state = %{dir: nil, max_bytes: Keyword.fetch!(opts, :max_bytes), bytes: 0}

    state =
      with :ok <- File.mkdir_p(dir),
           {:ok, names} <- File.ls(dir) do
        # A .tmp file is a write that a crash cut short.
        for name <- names, name =~ ~r/^[0-9a-f]{64}\.tmp$/, do: File.rm(Path.join(dir, name))
        %{state | dir: dir, bytes: dir |> thumbs(names) |> total()}
      else
        {:error, reason} ->
          Logger.warning("running without a thumbnail cache: #{dir}: #{inspect(reason)}")
          state
      end

    # Where lookup/2 finds the directory without asking this process. nil: no cache.
    :persistent_term.put({__MODULE__, name(opts)}, state.dir)
    {:ok, state}
  end

  @impl true
  def handle_cast({:put, _key, _webp}, %{dir: nil} = state), do: {:noreply, state}

  # The paths are the configured cache directory plus a key, and key/3 makes that from a SHA-256.
  # sobelow_skip ["Traversal.FileModule"]
  def handle_cast({:put, key, webp}, %{dir: dir} = state) do
    path = path(dir, key)
    tmp = Path.join(dir, key <> ".tmp")

    # The rename is atomic, so a reader sees the whole file or none. mkdir_p: a /tmp cleaner may
    # have removed an idle cache directory.
    with false <- File.exists?(path),
         :ok <- File.mkdir_p(dir),
         :ok <- File.write(tmp, webp),
         :ok <- File.rename(tmp, path) do
      {:noreply, evict(%{state | bytes: state.bytes + byte_size(webp)}, path)}
    else
      true ->
        {:noreply, state}

      {:error, reason} ->
        Logger.warning("thumbnail cache write failed: #{path}: #{inspect(reason)}")
        {:noreply, state}
    end
  end

  defp name(opts), do: Keyword.get(opts, :name, __MODULE__)

  defp path(dir, key), do: Path.join(dir, key <> ".webp")

  defp evict(%{bytes: bytes, max_bytes: max_bytes} = state, _keep) when bytes <= max_bytes, do: state

  # Recounts from the directory, because the running total goes stale when files are deleted from
  # outside. Deletes oldest first, down to 90% of the cap, so the next put does not evict again.
  # Only the `path != keep` guard saves the thumbnail just put: file times have one-second
  # resolution, so it can sort before older ones.
  # The paths are the key-shaped .webp names listed in the configured cache directory.
  # sobelow_skip ["Traversal.FileModule"]
  defp evict(%{dir: dir, max_bytes: max_bytes} = state, keep) do
    thumbs = dir |> thumbs(File.ls!(dir)) |> Enum.sort()
    target = div(max_bytes * 9, 10)

    bytes =
      Enum.reduce(thumbs, total(thumbs), fn
        {_mtime, path, size}, bytes when bytes > target and path != keep ->
          _ = File.rm(path)
          bytes - size

        _thumb, bytes ->
          bytes
      end)

    %{state | bytes: bytes}
  end

  # Only names shaped like a key: anything else in the directory is not this cache's to count or delete.
  defp thumbs(dir, names) do
    for name <- names,
        name =~ ~r/^[0-9a-f]{64}\.webp$/,
        path = Path.join(dir, name),
        {:ok, stat} <- [File.stat(path, time: :posix)],
        do: {stat.mtime, path, stat.size}
  end

  defp total(thumbs), do: thumbs |> Enum.map(&elem(&1, 2)) |> Enum.sum()
end
