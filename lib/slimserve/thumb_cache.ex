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

  # The listing's ETag comes without quotes and HEAD's with them, so both name one thumbnail.
  def key(s3_key, etag, max_dim) when is_binary(etag), do: digest({:s3, s3_key, String.trim(etag, ~s(")), max_dim})

  # :dims: an entry written before it carries no pixel size. Under the old key it is never read
  # again, so it misses once and ages out like any idle entry.
  defp digest(term), do: :sha256 |> :crypto.hash(:erlang.term_to_binary({:dims, term})) |> Base.encode16(case: :lower)

  @doc "One that is evicted before it is read is a miss."
  @spec lookup(atom(), key()) :: {:ok, binary()} | :miss
  def lookup(name \\ __MODULE__, key) do
    case read(name, key) do
      {:ok, webp, _dims} -> {:ok, webp}
      :miss -> :miss
    end
  end

  @doc """
  Stores a thumbnail, then deletes the oldest ones while the cache is over its cap.
  Does not wait for either. The size shares the file so the two are evicted together and the inspector
  can show it without opening the source.
  """
  @spec put(atom(), key(), binary(), {pos_integer(), pos_integer()}) :: :ok
  # No default name: it would define a put/3 that the old put(name, key, webp) still compiles against.
  def put(name, key, webp, {w, h}), do: DiskCache.put(name, key, <<w::32, h::32, webp::binary>>)

  @spec size(atom(), key()) :: {pos_integer(), pos_integer()} | nil
  def size(name \\ __MODULE__, key) do
    case read(name, key) do
      {:ok, _webp, dims} -> dims
      :miss -> nil
    end
  end

  defp read(name, key) do
    case DiskCache.read(name, key) do
      {:ok, <<w::32, h::32, webp::binary>>} -> {:ok, webp, {w, h}}
      _ -> :miss
    end
  end
end
