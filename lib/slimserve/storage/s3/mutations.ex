defmodule SlimServe.Storage.S3.Mutations do
  @moduledoc """
  S3 cannot reserve a name as both a file and a folder at once,
  so a conflict seen here is refused and one that lands after the check is not prevented.

  A no-clobber guarantee is sent to the provider only when `SlimServe.Storage.S3.Capabilities`
  showed at start that it enforces it. Upload and new folder need theirs and are refused without it.
  Move and Delete instead check the ETag or the absence themselves just before acting, which leaves
  a short window for a concurrent change.
  """

  alias SlimServe.Root
  alias SlimServe.Storage
  alias SlimServe.Storage.S3
  alias SlimServe.Storage.S3.Capabilities
  alias SlimServe.Storage.S3.Client
  alias SlimServe.Storage.S3.Metadata

  @if_none_match [{"if-none-match", "*"}]
  # The largest part S3 copies in one request.
  @part 5 * 1024 * 1024 * 1024

  def upload(segments, name, tmp) do
    with true <- proven?(:put),
         {:ok, key} <- fresh(segments, name, false),
         {:ok, %File.Stat{size: size}} <- File.stat(tmp) do
      headers = [{"content-length", Integer.to_string(size)} | @if_none_match]
      put = Client.request(:put, key, body: File.stream!(tmp, 65_536), headers: headers)
      announce(written(put), [segments ++ [name]], [], [])
    else
      _ -> :error
    end
  end

  def mkdir(segments, name) do
    with true <- proven?(:mkdir),
         {:ok, key} <- fresh(segments, name, true) do
      put = Client.request(:put, key <> "/", body: "", headers: @if_none_match)
      announce(written(put), [segments ++ [name]], [], [])
    else
      _ -> :error
    end
  end

  defp written({:ok, %{status: 200}}), do: :ok
  defp written(_put), do: :error

  def move(segments, from, parent, name) do
    with {kind, _found} = source when kind in [:dir, :file] <- S3.resolve(segments ++ [from], true),
         true <- kind == :dir or Storage.upload_type?(name),
         {:ok, dest} <- fresh(parent, name, kind == :dir) do
      move_source(source, dest, segments ++ [from], parent ++ [name])
    else
      _ -> :error
    end
  end

  defp move_source({:file, object}, dest, from, to), do: announce(move_key(object, dest), [to], [from], [])

  # Ignore patterns are relative to their ignore file, so a moved folder could reveal an entry one
  # hides at its current path. Dot-names and other names the gate refuses stay hidden anywhere.
  defp move_source({:dir, prefix}, dest, from, to) do
    with {:ok, {objects, []}} <- S3.keys(prefix, nil),
         false <- hidden_inside?(from, prefix, objects) do
      outcome = Enum.reduce_while(objects, :none, &move_next(&1, &2, dest <> "/" <> rest(&1.key, prefix)))

      case outcome do
        :all -> announce(:ok, [to], [from], [from])
        # The folder emptied after it resolved.
        :none -> :error
        outcome -> announce(outcome, [to], [from], [from])
      end
    else
      _ -> :error
    end
  end

  defp move_next(object, moved, dest) do
    case {move_key(object, dest), moved} do
      {:ok, _moved} -> {:cont, :all}
      {:partial, _moved} -> {:halt, :partial}
      {:error, :none} -> {:halt, :error}
      {:error, :all} -> {:halt, :partial}
    end
  end

  defp hidden_inside?(from, prefix, objects) do
    paths =
      for object <- objects,
          rel = String.split(rest(object.key, prefix), "/"),
          {names, dir?} = if(List.last(rel) == "", do: {Enum.drop(rel, -1), true}, else: {rel, false}),
          names != [] and Enum.all?(names, &Root.name?/1),
          do: {from ++ names, dir?}

    case S3.prefetch_rules(paths, true) do
      {:ok, rules} -> Enum.any?(paths, fn {path, dir?} -> S3.ignored?(path, dir?, &Map.fetch!(rules, &1)) end)
      {:error, _reason} -> true
    end
  end

  def delete(segments, name) do
    path = segments ++ [name]

    case S3.resolve(path, true) do
      {:file, object} ->
        announce(delete_key(object.key, object.etag), [], [path], [])

      {:dir, prefix} ->
        with {:ok, {objects, []}} <- S3.keys(prefix, nil), do: delete_or_ask(objects, prefix, name, path)

      _ ->
        :error
    end
  end

  defp delete_or_ask(objects, prefix, name, path) do
    if Enum.all?(objects, &(&1.key == prefix)),
      do: delete_all(objects, path),
      else: {:confirm, confirm(name, objects)}
  end

  def confirm_delete(segments, name, asked) do
    path = segments ++ [name]

    with {:dir, prefix} <- S3.resolve(path, true),
         {:ok, {objects, []}} <- S3.keys(prefix, nil) do
      case confirm(name, objects) do
        ^asked -> delete_all(objects, path)
        now -> {:confirm, now}
      end
    else
      _ -> :error
    end
  end

  defp confirm(name, objects) do
    %{
      name: name,
      count: Enum.count(objects, &(not slash?(&1.key))),
      snapshot: :erlang.phash2(for(object <- objects, do: {object.key, object.etag}))
    }
  end

  # Every key is tried; one that fails is reported, not rolled back.
  defp delete_all(objects, path) do
    results = for object <- objects, do: delete_key(object.key, object.etag)

    outcome =
      cond do
        Enum.all?(results, &(&1 == :ok)) -> :ok
        :ok in results -> :partial
        true -> :error
      end

    announce(outcome, [], [path], [path])
  end

  # A write whose answer was lost may still have landed.
  defp announce(outcome, added, removed, trees) do
    :ok = Metadata.changed(added, removed, trees)
    outcome
  end

  defp fresh(segments, name, dir?) do
    with true <- Root.name?(name),
         {:dir, prefix} <- S3.resolve(segments, true),
         false <- S3.ignored?(segments ++ [name], dir?, true),
         key = prefix <> name,
         :error <- S3.kind(key, true) do
      {:ok, key}
    else
      _ -> :error
    end
  end

  defp move_key(%{key: key, etag: etag} = object, dest) do
    case copy(object, dest) do
      :ok -> if delete_key(key, etag) == :ok, do: :ok, else: :partial
      _error -> :error
    end
  end

  # A part copy cannot take an empty object, and providers refuse a multipart upload to a key ending
  # in /: their bytes are read at the snapshot's ETag and written to a free key.
  defp copy(%{key: key, size: size} = object, dest) do
    if size == 0 or slash?(key), do: copy_bytes(object, dest), else: copy_multipart(object, dest)
  end

  defp copy_bytes(%{key: key, etag: etag, size: size}, dest) do
    guarantee = if(slash?(key), do: if(size == 0, do: :mkdir, else: :slash_put), else: :put)

    with {:ok, %{status: 200, body: body} = resp} <- Client.request(:get, key, headers: [{"if-match", etag}]),
         true <- Client.header(resp, "etag") == etag,
         {:ok, headers} <- absent(guarantee, dest),
         {:ok, %{status: 200}} <- Client.request(:put, dest, body: body, headers: headers) do
      :ok
    else
      _ -> :error
    end
  end

  defp copy_multipart(%{key: key, etag: etag, size: size}, dest) do
    with {:ok, upload} <- create_upload(dest) do
      result =
        with {:ok, parts} <- copy_parts(key, etag, size, dest, upload),
             {:ok, headers} <- absent(:complete, dest),
             do: complete(dest, upload, parts, headers)

      if result != :ok, do: abort(dest, upload)
      result
    end
  end

  defp copy_parts(key, etag, size, dest, upload) do
    0..(size - 1)//@part
    |> Enum.with_index(1)
    |> Enum.reduce_while({:ok, []}, fn {first, n}, {:ok, parts} ->
      range = "bytes=#{first}-#{min(first + @part, size) - 1}"

      with {:ok, if_match} <- unchanged(:part_copy, key, etag),
           {:ok, part} <- upload_part_copy(dest, upload, n, key, if_match, range) do
        {:cont, {:ok, parts ++ [{n, part}]}}
      else
        _ -> {:halt, :error}
      end
    end)
  end

  defp delete_key(key, etag) do
    with {:ok, if_match} <- unchanged(:delete, key, etag),
         {:ok, %{status: status}} when status in [200, 204] <- Client.request(:delete, key, headers: if_match) do
      :ok
    else
      _ -> :error
    end
  end

  defp unchanged(guarantee, key, etag) do
    header = if guarantee == :part_copy, do: "x-amz-copy-source-if-match", else: "if-match"

    cond do
      proven?(guarantee) -> {:ok, [{header, etag}]}
      etag(key) == etag -> {:ok, []}
      true -> :error
    end
  end

  defp etag(key) do
    case Client.request(:head, key) do
      {:ok, %{status: 200} = resp} -> Client.header(resp, "etag")
      _ -> nil
    end
  end

  defp absent(guarantee, key) do
    cond do
      proven?(guarantee) -> {:ok, @if_none_match}
      match?({:ok, %{status: 404}}, Client.request(:head, key)) -> {:ok, []}
      true -> :error
    end
  end

  defp proven?(guarantee), do: Capabilities.get()[guarantee] == true

  defp slash?(key), do: String.ends_with?(key, "/")

  defp rest(key, prefix), do: binary_part(key, byte_size(prefix), byte_size(key) - byte_size(prefix))

  defp create_upload(key) do
    with {:ok, %{status: 200} = resp} <- Client.request(:post, key, query: [uploads: ""]),
         %{"InitiateMultipartUploadResult" => %{"UploadId" => id}} <- Client.xml(resp) do
      {:ok, id}
    else
      other -> S3.upstream(other)
    end
  end

  def upload_part_copy(key, upload, n, source, headers, range) do
    headers = [{"x-amz-copy-source", Client.copy_source(source)}, {"x-amz-copy-source-range", range} | headers]

    # A copy can fail after a 200, with an error document as its body.
    with {:ok, %{status: 200} = resp} <-
           Client.request(:put, key, query: [partNumber: n, uploadId: upload], headers: headers),
         %{"CopyPartResult" => %{"ETag" => etag}} <- Client.xml(resp) do
      {:ok, etag}
    else
      other -> S3.upstream(other)
    end
  end

  def complete(key, upload, parts, headers) do
    # A completion can fail after a 200, with an error document as its body.
    with {:ok, %{status: 200} = resp} <-
           Client.request(:post, key, query: [uploadId: upload], body: completion(parts), headers: headers),
         %{"CompleteMultipartUploadResult" => _result} <- Client.xml(resp) do
      :ok
    else
      other -> S3.upstream(other)
    end
  end

  def completion(parts) do
    "<CompleteMultipartUpload>" <>
      Enum.map_join(parts, fn {n, etag} -> "<Part><PartNumber>#{n}</PartNumber><ETag>#{escape(etag)}</ETag></Part>" end) <>
      "</CompleteMultipartUpload>"
  end

  def abort(key, upload), do: Client.request(:delete, key, query: [uploadId: upload])

  defp escape(text),
    do: text |> String.replace("&", "&amp;") |> String.replace("\"", "&quot;") |> String.replace("<", "&lt;")
end
