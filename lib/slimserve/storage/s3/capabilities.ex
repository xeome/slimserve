defmodule SlimServe.Storage.S3.Capabilities do
  @moduledoc """
  S3-compatible providers differ in which conditional writes they honour, and one that ignores a
  condition overwrites silently. So, before the endpoint serves, the app writes its own objects under
  a hidden `.slimserve-probe/<run>/` prefix of the served bucket, tries each refused write the
  mutations rely on, and counts a guarantee only when the exact key was occupied, the write was
  refused with 412, and the original bytes are still there.

  A request whose answer was lost has no ID: only for those are the versions or uploads of that one
  run-owned key listed and removed, filtered to that exact key.
  """
  alias SlimServe.Storage.S3.Client
  alias SlimServe.Storage.S3.Mutations

  require Logger

  @prefix ".slimserve-probe/"
  @if_none_match [{"if-none-match", "*"}]
  @stale ~s("00000000000000000000000000000000")

  def child_spec(_opts), do: %{id: __MODULE__, start: {__MODULE__, :start_link, []}, restart: :temporary}

  # A provider not reached now would be counted short of guarantees for the pod's whole life, so boot fails.
  def start_link do
    case check() do
      {:error, :unreachable} = error -> error
      _capabilities -> :ignore
    end
  end

  @doc "The guarantees found at start: an empty map before any check, so nothing unproven is relied on."
  @spec get() :: %{atom() => boolean()}
  def get, do: Application.get_env(:slimserve, :s3_capabilities, %{})

  def check do
    run = @prefix <> Base.encode16(:crypto.strong_rand_bytes(8), case: :lower) <> "/"

    {results, made} =
      Enum.reduce_while(checks(), {[], []}, fn {guarantee, check}, {results, made} ->
        {shown?, writes} = check.(run <> Atom.to_string(guarantee) <> "/")
        acc = {results ++ [{guarantee, shown?}], made ++ writes}
        if shown? == :unreachable, do: {:halt, acc}, else: {:cont, acc}
      end)

    remove(made)

    if List.keymember?(results, :unreachable, 1), do: {:error, :unreachable}, else: record(results)
  end

  defp record(results) do
    capabilities = Map.new(results)
    Application.put_env(:slimserve, :s3_capabilities, capabilities)

    for {guarantee, false} <- results do
      Logger.warning("S3 provider did not show the #{guarantee} no-clobber guarantee: #{consequence(guarantee)}")
    end

    capabilities
  end

  defp consequence(:put), do: "uploads are refused"
  defp consequence(:mkdir), do: "new folders are refused"
  defp consequence(_guarantee), do: "move and delete check the object themselves just before acting"

  defp checks do
    [
      put: &refused(&1 <> "key", "original", fn key -> put(key, "clobber", @if_none_match) end),
      mkdir: fn dir ->
        both(
          refused(dir <> "empty/", "", fn key -> put(key, "", @if_none_match) end),
          refused(dir <> "full/", "original", fn key -> put(key, "", @if_none_match) end)
        )
      end,
      slash_put: fn dir ->
        both(
          refused(dir <> "empty/", "", fn key -> put(key, "clobber", @if_none_match) end),
          refused(dir <> "full/", "original", fn key -> put(key, "clobber", @if_none_match) end)
        )
      end,
      part_copy: &part_copy/1,
      complete: &complete/1,
      delete: &refused(&1 <> "key", "original", fn key -> delete(key, [{"if-match", @stale}]) end)
    ]
  end

  defp both({a?, a_writes}, {b?, b_writes}) do
    shown? = if :unreachable in [a?, b?], do: :unreachable, else: a? and b?
    {shown?, a_writes ++ b_writes}
  end

  defp refused(key, original, attempt) do
    with {true, setup} <- occupy(key, original) do
      {status, attempted} = attempt.(key)
      {status == 412 and read(key) == original, setup ++ attempted}
    end
  end

  # One answered but not readable back is the provider's doing (S3-D4).
  defp occupy(key, body) do
    {status, writes} = put(key, body, [])

    cond do
      read(key) == body -> {true, writes}
      status in 200..299 -> {false, writes}
      true -> {:unreachable, writes}
    end
  end

  # The part copy's source is fine; only its stated ETag is stale. Nothing is completed.
  defp part_copy(dir) do
    source = dir <> "source"

    with {true, setup} <- occupy(source, "original") do
      part_copy_onto(dir <> "copy", source, setup)
    end
  end

  defp part_copy_onto(key, source, setup) do
    case upload(key) do
      {:ok, id, created} ->
        status =
          status(
            Mutations.upload_part_copy(
              key,
              id,
              1,
              source,
              [{"x-amz-copy-source-if-match", @stale}],
              "bytes=0-7"
            )
          )

        {status == 412, setup ++ created}

      {:error, created} ->
        {false, setup ++ created}
    end
  end

  defp complete(dir) do
    source = dir <> "source"

    with {true, setup} <- occupy(source, "parts") do
      {shown?, writes} = refused(dir <> "key", "original", &complete_onto(&1, source))
      {shown?, setup ++ writes}
    end
  end

  defp complete_onto(key, source) do
    case upload(key) do
      {:ok, id, created} ->
        case Mutations.upload_part_copy(key, id, 1, source, [], "bytes=0-4") do
          {:ok, part} ->
            result =
              Client.request(:post, key,
                query: [uploadId: id],
                body: Mutations.completion([{1, part}]),
                headers: @if_none_match
              )

            {status(result), created ++ object(key, result)}

          _error ->
            {nil, created}
        end

      {:error, created} ->
        {nil, created}
    end
  end

  defp upload(key) do
    result = Client.request(:post, key, query: [uploads: ""])

    case result do
      {:ok, %{status: 200} = resp} ->
        case Client.xml(resp) do
          %{"InitiateMultipartUploadResult" => %{"UploadId" => id}} -> {:ok, id, [{:upload, key, id}]}
          _unknown -> {:error, [{:upload, key, :unknown}]}
        end

      {:ok, _refused} ->
        {:error, []}

      {:error, _lost} ->
        {:error, [{:upload, key, :unknown}]}
    end
  end

  defp put(key, body, headers) do
    result = Client.request(:put, key, body: body, headers: headers)
    {status(result), object(key, result)}
  end

  defp delete(key, headers) do
    result = Client.request(:delete, key, headers: headers)
    {status(result), object(key, result)}
  end

  # The version a request made: its ID, nil for an unversioned bucket, :unknown when the answer was lost.
  defp object(key, {:ok, %{status: status} = resp}) when status in 200..299,
    do: [{:object, key, Client.header(resp, "x-amz-version-id")}]

  defp object(_key, {:ok, _refused}), do: []
  defp object(key, {:error, _lost}), do: [{:object, key, :unknown}]

  defp status({:ok, %{status: status}}), do: status
  defp status({:error, {:status, status}}), do: status
  defp status(_other), do: nil

  defp read(key) do
    case Client.request(:get, key) do
      {:ok, %{status: 200, body: body}} -> IO.iodata_to_binary(body)
      _ -> nil
    end
  end

  defp remove(made) do
    failed = for made <- Enum.uniq(made), remove_one(made) != :ok, do: made
    if failed != [], do: Logger.error("S3 capability check could not remove its objects: #{inspect(failed)}")
  end

  defp remove_one({:object, key, nil}), do: removed(Client.request(:delete, key))

  defp remove_one({:object, key, id}) when is_binary(id),
    do: removed(Client.request(:delete, key, query: [versionId: id]))

  defp remove_one({:object, key, :unknown}) do
    with {:ok, ids} <- versions(key) do
      if Enum.all?(ids, &(remove_one({:object, key, &1}) == :ok)), do: :ok, else: :error
    end
  end

  defp remove_one({:upload, key, id}) when is_binary(id), do: removed(Mutations.abort(key, id))

  defp remove_one({:upload, key, :unknown}) do
    with {:ok, %{status: 200} = resp} <-
           Client.request(:get, "", query: [uploads: "", prefix: key, "encoding-type": "url"]),
         %{"ListMultipartUploadsResult" => %{} = result} <- Client.xml(resp) do
      uploads = List.wrap(result["Upload"])
      ids = for %{"Key" => k, "UploadId" => id} <- uploads, URI.decode_www_form(k) == key, do: id
      if Enum.all?(ids, &(remove_one({:upload, key, &1}) == :ok)), do: :ok, else: :error
    else
      _ -> :error
    end
  end

  # The listing's prefix also matches longer keys.
  defp versions(key) do
    with {:ok, %{status: 200} = resp} <-
           Client.request(:get, "", query: [versions: "", prefix: key, "encoding-type": "url"]),
         %{"ListVersionsResult" => %{} = result} <- Client.xml(resp) do
      entries = List.wrap(result["Version"]) ++ List.wrap(result["DeleteMarker"])
      {:ok, for(%{"Key" => k, "VersionId" => id} <- entries, URI.decode_www_form(k) == key, do: id)}
    end
  end

  # 404: already gone, as after a lost answer whose request never landed.
  defp removed({:ok, %{status: status}}) when status in [200, 204, 404], do: :ok
  defp removed(_other), do: :error
end
