defmodule SlimServe.Storage do
  @moduledoc """
  Every read and write of the web layer goes through here; each backend applies the
  same name gate (`SlimServe.Root.name?/1`) and ignore walker (`SlimServe.Ignore`).

  A read answers `:error` for a path that is absent or refused, and `{:error, reason}` when the store
  could not be asked, so a caller can tell a missing folder from an outage.
  """

  alias SlimServe.Storage.Local
  alias SlimServe.Storage.S3

  @type entry :: %{
          name: String.t(),
          dir?: boolean(),
          size: non_neg_integer(),
          mtime: integer(),
          thumb: String.t() | nil
        }

  @type result :: :ok | :partial | :error

  @callback resolve([String.t()]) :: {:dir, term()} | {:file, Path.t() | map()} | :error | {:error, term()}
  @callback list([String.t()], keyword()) :: {:ok, [entry()]} | :error | {:error, term()}
  @callback topics([String.t()]) :: [String.t()]
  @callback upload([String.t()], String.t(), Path.t()) :: result()
  @callback mkdir([String.t()], String.t()) :: result()
  @callback move([String.t()], String.t(), [String.t()], String.t()) :: result()
  @callback delete([String.t()], String.t()) :: result() | {:confirm, map()}
  @callback confirm_delete([String.t()], String.t(), map()) :: result() | {:confirm, map()}

  def s3?, do: Application.get_env(:slimserve, :storage, :local) == :s3

  defp backend, do: if(s3?(), do: S3, else: Local)

  @spec resolve([String.t()]) :: {:dir, term()} | {:file, Path.t() | map()} | :error | {:error, term()}
  def resolve(segments), do: backend().resolve(segments)

  @doc "`fresh: true` skips S3's metadata cache."
  @spec list([String.t()], fresh: boolean()) :: {:ok, [entry()]} | :error | {:error, term()}
  def list(segments, opts \\ []), do: backend().list(segments, opts)

  def subscribe(segments) do
    for topic <- backend().topics(segments), do: :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, topic)
    :ok
  end

  @spec upload([String.t()], String.t(), Path.t()) :: result()
  def upload(segments, name, tmp), do: if(upload_type?(name), do: backend().upload(segments, name, tmp), else: :error)

  @spec mkdir([String.t()], String.t()) :: result()
  def mkdir(segments, name), do: backend().mkdir(segments, name)

  @spec move([String.t()], String.t(), String.t()) :: result()
  def move(segments, from, to) do
    source = segments ++ [from]

    with {parent, [name]} <- to |> String.split("/", trim: true) |> Enum.split(-1),
         false <- Enum.take(parent ++ [name], length(source)) == source do
      backend().move(segments, from, parent, name)
    else
      _ -> :error
    end
  end

  @spec delete([String.t()], String.t()) :: result() | {:confirm, map()}
  def delete(segments, name), do: backend().delete(segments, name)

  @spec confirm_delete([String.t()], String.t(), map()) :: result() | {:confirm, map()}
  def confirm_delete(segments, name, asked), do: backend().confirm_delete(segments, name, asked)

  # allow_upload's accept also passes a file by the type the browser claims, so the name is checked here.
  @doc false
  def upload_type?(name) do
    case Application.get_env(:slimserve, :upload_types, :any) do
      :any -> true
      types -> String.downcase(Path.extname(name)) in types
    end
  end
end
