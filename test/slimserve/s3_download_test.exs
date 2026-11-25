defmodule SlimServe.S3DownloadTest do
  # Not async: the storage backend is application env and the download cache a named process.
  use SlimServeWeb.ConnCase, async: false

  alias SlimServe.DiskCache
  alias SlimServe.S3Fixture, as: S3

  @moduletag :s3

  @digits "0123456789"
  @cap 1_000

  setup do
    test = self()
    rules = start_supervised!({Agent, fn -> fn _request -> :pass end end})

    endpoint =
      S3.proxy(fn request ->
        send(test, {:proxied, request})
        Agent.get(rules, & &1).(request)
      end)

    bucket = S3.serve(endpoint: endpoint, cache_bytes: @cap)
    S3.put(bucket, "digits.txt", @digits)
    {:ok, bucket: bucket, rules: rules}
  end

  defp ranged(conn, nil), do: get(conn, "/digits.txt")
  defp ranged(conn, range), do: conn |> put_req_header("range", range) |> get("/digits.txt")

  defp header(conn, name), do: conn |> get_resp_header(name) |> List.first()

  defp fill_cache, do: {:ok, _tmp} = DiskCache.reserve(SlimServe.DownloadCache, @cap)

  # Only the object's own GET: the folder check's listing names the key in its query too.
  defp on_get(rules, rule) do
    only = fn request -> if object_get?(request), do: rule, else: :pass end
    Agent.update(rules, fn _rule -> only end)
  end

  defp object_get?(request),
    do: request.method == "GET" and String.ends_with?(URI.parse(request.path).path, "/digits.txt")

  test "a small file is fetched once, then every full and ranged answer comes from the cache", %{conn: conn} do
    full = ranged(conn, nil)
    assert {full.status, full.resp_body, header(full, "x-cache")} == {200, @digits, "miss"}
    assert header(full, "content-type") == "text/plain"
    assert header(full, "accept-ranges") == "bytes"

    S3.watch_requests()

    for {range, status, body, content_range} <- [
          {nil, 200, @digits, nil},
          {"bytes=2-5", 206, "2345", "bytes 2-5/10"},
          {"bytes=-3", 206, "789", "bytes 7-9/10"},
          {"bytes=8-", 206, "89", "bytes 8-9/10"},
          {"bytes=10-", 416, "", "bytes */10"}
        ] do
      conn = ranged(conn, range)
      assert {conn.status, conn.resp_body, header(conn, "content-range")} == {status, body, content_range}, inspect(range)
      if status != 416, do: assert(header(conn, "x-cache") == "hit")
    end

    refute_received {:s3_request, :get, _key, _query}
  end

  test "a miss the cache has no room for streams, with the normalized range pinned to the ETag", %{
    conn: conn,
    bucket: bucket
  } do
    [etag] = bucket |> S3.raw(:head, "digits.txt") |> Req.Response.get_header("etag")
    path = "/#{bucket}/digits.txt"
    fill_cache()

    conn = ranged(conn, "bytes=-3")
    assert {conn.status, conn.resp_body, header(conn, "x-cache")} == {206, "789", "stream"}
    assert {header(conn, "content-range"), header(conn, "content-length")} == {"bytes 7-9/10", "3"}
    assert_received {:proxied, %{method: "GET", path: ^path, headers: headers}}
    assert {"range", "bytes=7-9"} in headers
    assert {"if-match", etag} in headers

    whole = ranged(conn, nil)
    assert {whole.status, whole.resp_body, header(whole, "x-cache")} == {200, @digits, "stream"}
  end

  test "a streamed answer with the wrong length, range or ETag is refused before any header", %{conn: conn, rules: rules} do
    fill_cache()

    for {name, value} <- [{"content-length", "5"}, {"content-range", "bytes 0-3/10"}, {"etag", ~s("other")}] do
      on_get(rules, {:header, name, value})
      conn = ranged(conn, "bytes=2-5")

      assert {conn.status, header(conn, "content-range")} == {502, nil},
             "#{name}: #{conn.status} #{inspect(conn.resp_body)}"
    end
  end

  @tag capture_log: true
  test "a streamed body cut short closes an HTTP/1 connection and resets an HTTP/2 stream", %{rules: rules} do
    fill_cache()
    on_get(rules, {:truncate, 4})
    server = start_supervised!({Bandit, plug: SlimServeWeb.Endpoint, ip: :loopback, port: 0, startup_log: false})
    {:ok, {_ip, port}} = ThousandIsland.listener_info(server)

    # HTTP/1 has no stream to reset: the connection closes.
    {:ok, http1} = Mint.HTTP1.connect(:http, "127.0.0.1", port)
    assert {:error, %Mint.TransportError{reason: :closed}, responses} = fetch(http1)
    assert [{:status, _ref, 200} | _rest] = responses
    assert byte_size(body(responses)) < byte_size(@digits)

    # HTTP/2 resets the one stream; Mint reports that as the request's error.
    {:ok, http2} = Mint.HTTP2.connect(:http, "127.0.0.1", port)
    assert {:error, %Mint.HTTPError{reason: {:server_closed_request, :internal_error}}, responses} = fetch(http2)
    assert [{:status, _ref, 200} | _rest] = responses
    assert byte_size(body(responses)) < byte_size(@digits)
  end

  # Within two seconds, so a hang that would end only at a timeout fails.
  defp fetch(conn) do
    {:ok, conn, _ref} = Mint.HTTP.request(conn, "GET", "/digits.txt", [], nil)
    collect(conn, [], System.monotonic_time(:millisecond) + 2_000)
  end

  defp collect(conn, acc, deadline) do
    receive do
      message ->
        case Mint.HTTP.stream(conn, message) do
          {:ok, conn, responses} ->
            acc = acc ++ responses

            case Enum.find(acc, &(elem(&1, 0) in [:done, :error])) do
              {:error, _ref, error} -> {:error, error, acc -- [{:error, elem(hd(acc), 1), error}]}
              {:done, _ref} -> {:done, acc}
              nil -> collect(conn, acc, deadline)
            end

          {:error, _conn, error, responses} ->
            {:error, error, acc ++ responses}

          :unknown ->
            collect(conn, acc, deadline)
        end
    after
      max(deadline - System.monotonic_time(:millisecond), 0) -> {:timeout, acc}
    end
  end

  defp body(responses), do: for({:data, _ref, data} <- responses, into: "", do: data)
end
