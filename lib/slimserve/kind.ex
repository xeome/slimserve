defmodule SlimServe.Kind do
  @moduledoc """
  For the listing's Kind column, its category card and its
  inspector. Separate from `SlimServe.Thumbnail.image?/1`, which gates thumbnails only.
  """

  @type t :: :folder | :image | :video | :audio | :document | :archive | :code | :other

  @kinds %{
    image: ~w(.jpg .jpeg .png .gif .webp .svg .bmp .tif .tiff .avif .heic .ico),
    video: ~w(.mp4 .m4v .mkv .webm .mov .avi .wmv .mpg .mpeg),
    audio: ~w(.mp3 .flac .wav .ogg .oga .opus .m4a .aac),
    document: ~w(.pdf .txt .md .rtf .doc .docx .odt .xls .xlsx .ods .csv .ppt .pptx .odp .epub),
    archive: ~w(.zip .tar .gz .tgz .bz2 .xz .zst .7z .rar),
    code: ~w(.ex .exs .erl .js .ts .py .rb .go .rs .c .h .cpp .java .sh .json .yaml .yml .toml .html .css .xml .sql)
  }

  @by_ext for {kind, exts} <- @kinds, ext <- exts, into: %{}, do: {ext, kind}

  @doc "In the order the page lists them: one hue each, `--s1` on."
  def order, do: ~w(image video audio document archive code other)a

  @spec of(String.t(), boolean()) :: t()
  def of(_name, true), do: :folder
  def of(name, false), do: Map.get(@by_ext, String.downcase(Path.extname(name)), :other)

  def label(:folder), do: "Folder"
  def label(:other), do: "File"
  def label(kind), do: kind |> Atom.to_string() |> String.capitalize()

  def plural(:audio), do: "Audio"
  def plural(:code), do: "Code"
  def plural(:other), do: "Other"
  def plural(kind), do: label(kind) <> "s"
end
