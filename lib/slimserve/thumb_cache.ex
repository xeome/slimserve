defmodule SlimServe.ThumbCache do
  @moduledoc """
  A hit is read by the caller and sends the cache process nothing, so a busy or restarting cache still
  serves. Only the cache process writes, so two requests that miss the same image cannot interleave
  their writes. libvips runs in the caller.
  """

  alias SlimServe.DiskCache

  @type key :: String.t()

  def child_spec(opts), do: %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}}

  @doc "Starts the cache on `:dir` with the cap `:max_bytes`, registered as `:name` (this module by default)."
  def start_link(opts), do: DiskCache.start_link(Keyword.merge([name: __MODULE__, ext: "webp"], opts))

  @doc """
  A local file's key changes when its path, inode, times or size do, so a replacement that keeps the
  size and the modification time still misses. An S3 object's is its key and ETag, so a replaced
  object misses too.
  """
  @spec key(Path.t(), File.Stat.t() | String.t(), pos_integer()) :: key()
  def key(path, %File.Stat{inode: inode, ctime: ctime, mtime: mtime, size: size}, max_dim),
    do: digest({path, inode, ctime, mtime, size, max_dim})

  def key(s3_key, etag, max_dim) when is_binary(etag), do: digest({:s3, s3_key, etag, max_dim})

  defp digest(term), do: :sha256 |> :crypto.hash(:erlang.term_to_binary(term)) |> Base.encode16(case: :lower)

  @doc "The cached thumbnail's bytes, read in the caller. One that is evicted before it is read is a miss."
  @spec lookup(atom(), key()) :: {:ok, binary()} | :miss
  def lookup(name \\ __MODULE__, key), do: DiskCache.read(name, key)

  @doc "Stores a thumbnail, then deletes the oldest ones while the cache is over its cap. Does not wait for either."
  @spec put(atom(), key(), binary()) :: :ok
  def put(name \\ __MODULE__, key, webp), do: DiskCache.put(name, key, webp)
end
