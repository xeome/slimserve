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
  The source's own width and height as shown, after its EXIF orientation.

  A file whose content is not a JPEG, PNG, GIF or WebP is an error, whatever its name.
  """
  @spec generate(Path.t(), pos_integer()) ::
          {:ok, binary(), {pos_integer(), pos_integer()}} | {:error, term()}
  def generate(path, max_dim) do
    with :ok <- check_loader(path),
         {:ok, source} <- Image.new_from_file(path),
         {:ok, image} <- Operation.thumbnail(path, max_dim, size: :VIPS_SIZE_DOWN),
         {:ok, webp} <- Image.write_to_buffer(image, ".webp", keep: [:VIPS_FOREIGN_KEEP_ICC]) do
      {:ok, webp, shown_size(source)}
    end
  end

  # Orientations 5 to 8 turn the image a quarter.
  defp shown_size(source) do
    {w, h} = {Image.width(source), Image.height(source)}

    case Image.header_value(source, "orientation") do
      {:ok, turn} when turn in 5..8 -> {h, w}
      _ -> {w, h}
    end
  end

  @doc """
  From its header alone. A truncated file passes this and still fails to render.
  """
  @spec loadable?(Path.t()) :: boolean()
  def loadable?(path), do: check_loader(path) == :ok

  defp check_loader(path) do
    case Foreign.find_load(path) do
      {:ok, loader} when loader in @loaders -> :ok
      {:ok, loader} -> {:error, {:loader, loader}}
      {:error, reason} -> {:error, reason}
    end
  end
end
