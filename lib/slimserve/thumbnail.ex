defmodule SlimServe.Thumbnail do
  @moduledoc "Makes WebP thumbnails with libvips, through Vix."

  alias Vix.Vips.Foreign
  alias Vix.Vips.Image
  alias Vix.Vips.Operation

  @extensions ~w(.jpg .jpeg .png .gif .webp)

  # libvips picks its loader from the file's content, not its name. So an SVG named x.png would go
  # to the SVG renderer, and that follows an href to a hidden file beside it. Only these loaders run.
  @loaders ~w(VipsForeignLoadJpegFile VipsForeignLoadPngFile VipsForeignLoadNsgifFile VipsForeignLoadWebpFile)

  @doc """
  Whether the file at `path` gets a thumbnail, by its extension.

  libvips reads a trailing `[...]` in a file name as load options. A path that ends in one of
  these extensions has none, so `generate/2` opens exactly the file it is given.
  """
  @spec image?(Path.t()) :: boolean()
  def image?(path), do: String.downcase(Path.extname(path)) in @extensions

  @doc """
  The image at `path` as a WebP that fits a square of `max_dim` pixels. A smaller image keeps
  its size. WebP keeps transparency. The WebP carries none of the source's metadata.

  A file whose content is not a JPEG, PNG, GIF or WebP is an error, whatever its name.
  """
  @spec generate(Path.t(), pos_integer()) :: {:ok, binary()} | {:error, term()}
  def generate(path, max_dim) do
    with {:ok, loader} when loader in @loaders <- Foreign.find_load(path),
         {:ok, image} <- Operation.thumbnail(path, max_dim, size: :VIPS_SIZE_DOWN) do
      Image.write_to_buffer(image, ".webp", keep: [:VIPS_FOREIGN_KEEP_ICC])
    else
      {:ok, loader} -> {:error, {:loader, loader}}
      {:error, reason} -> {:error, reason}
    end
  end
end
