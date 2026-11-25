defmodule SlimServe.Storage.S3.Download do
  @moduledoc """
  Sends an S3 object's bytes, for a range `SlimServeWeb.BrowseController` has already normalized.

  A larger object, or a miss the cache has no room for, is streamed: the request carries the range and
  the ETag, and the response's status, ETag, Content-Range and length are checked before any header goes
  out, so a wrong answer is a 502.

  A body that ends early or
  runs long raises one Bandit stream error: Bandit closes an HTTP/1 connection and resets an HTTP/2
  stream, so the client sees a broken response, never a short one that looks complete.
  """
  import Plug.Conn

  alias Bandit.HTTP2.Errors
  alias SlimServe.DiskCache
  alias SlimServe.Storage.S3.Client

  @cache SlimServe.DownloadCache
  @max_cached 100 * 1024 * 1024

  def send(conn, status, object, offset, length) do
    case cached(object) do
      {:ok, path, how} ->
        try do
          conn |> put_resp_header("x-cache", how) |> send_file(status, path, offset, length)
        after
          DiskCache.release(@cache, path)
        end

      :stream ->
        stream(conn, status, object, offset, if(length == :all, do: object.size, else: length))

      {:error, _reason} ->
        bad_gateway(conn)
    end
  end

  def with_file(object, fun) do
    case cached(object) do
      {:ok, path, _how} ->
        try do
          fun.(path)
        after
          DiskCache.release(@cache, path)
        end

      _stream_or_error ->
        {:error, :unavailable}
    end
  end

  defp cached(%{size: size} = object) when size <= @max_cached do
    key = :sha256 |> :crypto.hash(:erlang.term_to_binary({object.key, object.etag})) |> Base.encode16(case: :lower)

    with :miss <- DiskCache.claim(@cache, key),
         {:ok, tmp} <- DiskCache.reserve(@cache, size) do
      fetch(object, tmp, key)
    else
      {:ok, path} -> {:ok, path, "hit"}
      :full -> :stream
    end
  end

  defp cached(_object), do: :stream

  defp fetch(object, tmp, key) do
    result = Client.request(:get, object.key, headers: [{"if-match", object.etag}], into: File.stream!(tmp))

    with {:ok, %{status: 200} = resp} <- result,
         true <- Client.header(resp, "etag") == object.etag,
         {:ok, %File.Stat{size: size}} when size == object.size <- File.stat(tmp),
         {:ok, path} <- DiskCache.publish(@cache, tmp, key) do
      {:ok, path, "miss"}
    else
      other ->
        DiskCache.release(@cache, tmp)
        {:error, other}
    end
  end

  defp stream(conn, status, object, offset, length) do
    last = offset + length - 1
    range = if status == 206, do: [{"range", "bytes=#{offset}-#{last}"}], else: []

    with {:ok, resp} <- Client.request(:get, object.key, headers: [{"if-match", object.etag} | range], into: :self),
         true <- valid?(resp, status, object, "bytes #{offset}-#{last}/#{object.size}", length) || cancel(resp) do
      conn
      |> put_resp_header("x-cache", "stream")
      |> put_resp_header("content-length", Integer.to_string(length))
      |> send_chunked(status)
      |> relay(resp, length)
    else
      _ -> bad_gateway(conn)
    end
  end

  # The range the controller put on the headers is not this answer's.
  defp bad_gateway(conn), do: conn |> delete_resp_header("content-range") |> send_resp(502, "Bad Gateway")

  defp valid?(resp, status, object, content_range, length) do
    resp.status == status and Client.header(resp, "etag") == object.etag and
      Client.header(resp, "content-length") == Integer.to_string(length) and
      (status == 200 or Client.header(resp, "content-range") == content_range)
  end

  defp cancel(resp) do
    Req.cancel_async_response(resp)
    false
  end

  defp relay(conn, resp, length) do
    {conn, sent} =
      Enum.reduce(resp.body, {conn, 0}, fn data, {conn, sent} ->
        sent = sent + byte_size(data)
        if sent > length, do: abort()

        case chunk(conn, data) do
          {:ok, conn} -> {conn, sent}
          {:error, _closed} -> abort()
        end
      end)

    if sent != length, do: abort()
    conn
  rescue
    error in Errors.StreamError -> reraise error, __STACKTRACE__
    _upstream -> abort()
  end

  defp abort do
    raise Errors.StreamError, message: "the S3 body did not match its length", error_code: Errors.internal_error()
  end
end
