defmodule SlimServe.Storage.S3 do
  @moduledoc """
  A zero-byte `name/` object (a marker) keeps an empty folder. A key's names pass the same gate as a
  local path's (`SlimServe.Root.name?/1`) and the same ignore walker, which reads `.slimserveignore` objects.

  Reads go through `SlimServe.Storage.S3.Metadata`; a write asks again
  with `fresh?` true. An ignore object that cannot be read hides its folder, and one that could not be
  fetched makes the whole read `{:error, reason}`, not a refusal.
  """

  @behaviour SlimServe.Storage

  alias SlimServe.Ignore
  alias SlimServe.Root
  alias SlimServe.Storage.S3.Client
  alias SlimServe.Storage.S3.Metadata
  alias SlimServe.Storage.S3.Mutations
  alias SlimServe.Thumbnail

  @impl true
  defdelegate upload(segments, name, tmp), to: Mutations
  @impl true
  defdelegate mkdir(segments, name), to: Mutations
  @impl true
  defdelegate move(segments, from, parent, name), to: Mutations
  @impl true
  defdelegate delete(segments, name), to: Mutations
  @impl true
  defdelegate confirm_delete(segments, name, asked), to: Mutations

  @impl true
  def topics(segments), do: [Root.topic(Enum.join(segments, "/")), Metadata.tree_topic()]

  @impl true
  def resolve(segments, fresh? \\ false), do: resolve(segments, fresh?, &rules(&1, fresh?))

  defp resolve([], _fresh?, _rules_at), do: {:dir, ""}

  defp resolve(segments, fresh?, rules_at) do
    with true <- Enum.all?(segments, &Root.name?/1),
         {kind, _} = found when kind in [:dir, :file] <- kind(Enum.join(segments, "/"), fresh?),
         false <- ignored?(segments, kind == :dir, rules_at) do
      found
    else
      {:error, _reason} = error -> error
      _ -> :error
    end
  end

  @impl true
  def list(segments, opts) do
    fresh? = Keyword.get(opts, :fresh, false)

    with true <- Enum.all?(segments, &Root.name?/1),
         {:ok, rules} <- prefetch_rules([{segments, true}], fresh?),
         {:dir, prefix} <- resolve(segments, fresh?, &Map.fetch!(rules, &1)),
         {:ok, {objects, prefixes}} <- listing(prefix, fresh?),
         entries = for(entry <- entries(prefix, objects, prefixes), Root.name?(entry.name), do: entry),
         folders = for(%{dir?: true, name: name} <- entries, do: {segments ++ [name], true}),
         {:ok, rules} <- prefetch_rules(folders, fresh?, rules) do
      visible =
        for entry <- entries, not ignored?(segments ++ [entry.name], entry.dir?, &Map.fetch!(rules, &1)), do: entry

      {:ok, Enum.sort_by(visible, &{not &1.dir?, &1.name})}
    else
      {:error, _reason} = error -> error
      _ -> :error
    end
  end

  def ignored?(segments, dir?, fresh?) when is_boolean(fresh?), do: ignored?(segments, dir?, &rules(&1, fresh?))

  def ignored?(segments, dir?, rules_at) do
    Ignore.ignored_by?(rules_at, segments, dir?)
  catch
    {__MODULE__, reason} -> {:error, reason}
  end

  @doc """
  The rules of every folder `ignored?/3` reads for these paths.
  """
  @spec prefetch_rules([{[String.t()], boolean()}], boolean(), map()) :: {:ok, map()} | {:error, term()}
  def prefetch_rules(paths, fresh?, known \\ %{}) do
    folders =
      for {segments, dir?} <- paths,
          n <- 0..(length(segments) - if(dir?, do: 0, else: 1))//1,
          folder = Enum.take(segments, n),
          not Map.has_key?(known, folder),
          uniq: true,
          do: folder

    folders
    |> Task.async_stream(&{&1, fetch_rules(&1, fresh?)}, timeout: :infinity)
    |> Enum.reduce_while({:ok, known}, fn
      {:ok, {_folder, {:error, _reason} = error}}, _acc -> {:halt, error}
      {:ok, {folder, rules}}, {:ok, acc} -> {:cont, {:ok, Map.put(acc, folder, rules)}}
    end)
  end

  def listing(prefix, fresh?), do: Metadata.fetch({:list, prefix}, fresh?, fn -> keys(prefix, "/") end)

  @doc "Keys come URL-encoded, so one that XML cannot carry does not break the whole listing."
  def keys(prefix, delimiter), do: page(prefix, delimiter, nil, [], [])

  defp page(prefix, delimiter, token, objects, prefixes) do
    query = ["list-type": 2, prefix: prefix, "encoding-type": "url"]
    query = if delimiter, do: query ++ [delimiter: delimiter], else: query
    query = if token, do: query ++ ["continuation-token": token], else: query

    with_result =
      with {:ok, %{status: 200} = resp} <- Client.request(:get, "", query: query),
           %{"ListBucketResult" => %{} = result} <- Client.xml(resp) do
        objects = objects ++ for(object <- List.wrap(result["Contents"]), do: object(object))
        prefixes = prefixes ++ for(%{"Prefix" => p} <- List.wrap(result["CommonPrefixes"]), do: URI.decode_www_form(p))

        if result["IsTruncated"] == "true",
          do: page(prefix, delimiter, result["NextContinuationToken"], objects, prefixes),
          else: {:ok, {objects, prefixes}}
      end

    upstream(with_result)
  end

  def object(%{"Key" => key, "ETag" => etag, "Size" => size, "LastModified" => modified}) do
    {:ok, mtime, _offset} = DateTime.from_iso8601(modified)
    %{key: URI.decode_www_form(key), etag: etag, size: String.to_integer(size), mtime: DateTime.to_unix(mtime)}
  end

  def upstream({:ok, %Req.Response{status: status}}), do: {:error, {:status, status}}
  def upstream({:ok, _value} = ok), do: ok
  def upstream({:error, _reason} = error), do: error
  def upstream(other), do: {:error, {:unexpected, other}}

  defp entries(prefix, objects, prefixes) do
    folders = for p <- prefixes, do: binary_part(p, byte_size(prefix), byte_size(p) - byte_size(prefix) - 1)
    thumb_max_file_bytes = Application.fetch_env!(:slimserve, :thumb_max_file_bytes)

    files =
      for %{key: key} = object <- objects,
          name = binary_part(key, byte_size(prefix), byte_size(key) - byte_size(prefix)),
          name != "" and name not in folders do
        %{
          name: name,
          dir?: false,
          size: object.size,
          mtime: object.mtime,
          thumb: if(Thumbnail.image?(name) and object.size <= thumb_max_file_bytes, do: String.trim(object.etag, ~s(")))
        }
      end

    for(name <- folders, do: %{name: name, dir?: true, size: 0, mtime: nil, thumb: nil}) ++ files
  end

  def kind(key, fresh?) do
    case folder?(key, fresh?) do
      {:ok, false} -> stat(key, fresh?)
      {:ok, true} -> {:dir, key <> "/"}
      error -> error
    end
  end

  defp folder?(key, fresh?) do
    Metadata.fetch({:folder, key}, fresh?, fn ->
      with_result =
        with {:ok, %{status: 200} = resp} <-
               Client.request(:get, "", query: ["list-type": 2, prefix: key <> "/", "max-keys": 1]),
             %{"ListBucketResult" => %{"KeyCount" => count}} <- Client.xml(resp) do
          {:ok, count != "0"}
        end

      upstream(with_result)
    end)
  end

  def stat(key, fresh?) do
    Metadata.fetch({:stat, key}, fresh?, fn ->
      case Client.request(:head, key) do
        {:ok, %{status: 200} = resp} ->
          {:file,
           %{key: key, etag: Client.header(resp, "etag"), size: String.to_integer(Client.header(resp, "content-length"))}}

        {:ok, %{status: 404}} ->
          :error

        other ->
          upstream(other)
      end
    end)
  end

  defp rules(segments, fresh?) do
    case fetch_rules(segments, fresh?) do
      {:error, reason} -> throw({__MODULE__, reason})
      rules -> rules
    end
  end

  defp fetch_rules(segments, fresh?) do
    key = Enum.join(segments ++ [".slimserveignore"], "/")

    Metadata.fetch({:rules, key}, fresh?, fn ->
      case Client.request(:get, key) do
        {:ok, %{status: 200, body: body}} -> Ignore.parse(IO.iodata_to_binary(body))
        {:ok, %{status: 404}} -> []
        other -> upstream(other)
      end
    end)
  end
end
