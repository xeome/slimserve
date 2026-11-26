defmodule SlimServe.S3DownloadTest do
  # Not async: the storage backend is application env and the download cache a named process.
  use SlimServeWeb.ConnCase, async: false

  alias SlimServe.DiskCache
  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.ThumbCache
  alias Vix.Vips.Image

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

  test "Scenario: A small file is fetched once, then every full and ranged answer comes from the cache", %{conn: conn} do
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

  test "Scenario: A miss the cache has no room for streams, with the normalized range pinned to the ETag", %{
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

  test "Scenario: A HEAD that would stream answers with GET's status and headers but fetches no body", %{
    conn: conn,
    bucket: bucket
  } do
    path = "/#{bucket}/digits.txt"
    fill_cache()

    for range <- [nil, "bytes=2-5"] do
      got = ranged(conn, range)
      assert_received {:proxied, %{method: "GET", path: ^path}}

      head = if range, do: conn |> put_req_header("range", range) |> head("/digits.txt"), else: head(conn, "/digits.txt")
      refute_received {:proxied, %{method: "GET", path: ^path}}, inspect(range)
      assert {head.status, head.resp_body} == {got.status, ""}
      assert resp_headers(head) == resp_headers(got)
    end
  end

  defp resp_headers(conn), do: conn.resp_headers |> List.keydelete("x-request-id", 0) |> Enum.sort()

  test "Scenario: A HEAD on a cacheable file fetches and caches nothing on a miss, and answers a hit from the cache", %{
    conn: conn,
    bucket: bucket
  } do
    path = "/#{bucket}/digits.txt"

    head_ranged = fn range ->
      if range, do: conn |> put_req_header("range", range) |> head("/digits.txt"), else: head(conn, "/digits.txt")
    end

    ranges = [nil, "bytes=2-5"]

    heads = Enum.map(ranges, head_ranged)
    refute_received {:proxied, %{method: "GET", path: ^path}}

    [full, part] = gets = Enum.map(ranges, &ranged(conn, &1))
    assert {header(full, "x-cache"), header(part, "x-cache")} == {"miss", "hit"}
    assert_received {:proxied, %{method: "GET", path: ^path}}

    # The test adapter puts no content-length on a sent file.
    for {head, got} <- Enum.zip(heads, gets) do
      assert {head.status, head.resp_body, header(head, "content-length")} ==
               {got.status, "", Integer.to_string(byte_size(got.resp_body))}

      assert without(head, ~w(content-length x-cache)) == without(got, ~w(x-cache))
    end

    for {range, got} <- Enum.zip(ranges, gets) do
      hit = head_ranged.(range)
      assert {hit.status, hit.resp_body, header(hit, "x-cache")} == {got.status, "", "hit"}
      assert without(hit, ~w(x-cache)) == without(got, ~w(x-cache))
    end

    refute_received {:proxied, %{method: "GET", path: ^path}}
  end

  defp without(conn, names), do: Enum.reject(resp_headers(conn), &(elem(&1, 0) in names))

  @tag :tmp_dir
  test "Scenario: A thumbnail HEAD renders nothing on a miss and sends no length, and a hit's length", %{
    bucket: bucket,
    tmp_dir: tmp_dir
  } do
    :ok = Supervisor.terminate_child(SlimServe.Supervisor, ThumbCache)
    on_exit(fn -> {:ok, _pid} = Supervisor.restart_child(SlimServe.Supervisor, ThumbCache) end)
    start_supervised!({ThumbCache, dir: tmp_dir, max_bytes: 1024 * 1024})

    {:ok, png} = 8 |> Image.build_image!(8, [200, 80, 40]) |> Image.write_to_buffer(".png")
    S3.put(bucket, "pic.png", png)
    path = "/#{bucket}/pic.png"
    port = start_bandit()

    thumb = fn http, method ->
      :http |> http.connect("127.0.0.1", port) |> elem(1) |> fetch(method, [], "/pic.png?thumb=1")
    end

    for http <- [Mint.HTTP1, Mint.HTTP2] do
      assert {:done, head} = thumb.(http, "HEAD")
      refute_received {:proxied, %{method: "GET", path: ^path}}, inspect(http)

      assert {wire_status(head), wire_header(head, "content-type"), wire_header(head, "content-length"), body(head)} ==
               {[200], ["image/webp"], [], ""},
             inspect(http)
    end

    assert {:done, get} = thumb.(Mint.HTTP1, "GET")
    assert_received {:proxied, %{method: "GET", path: ^path}}
    length = [Integer.to_string(byte_size(body(get)))]
    assert {wire_header(get, "x-cache"), wire_header(get, "content-length")} == {["miss"], length}
    _ = :sys.get_state(ThumbCache)

    for http <- [Mint.HTTP1, Mint.HTTP2] do
      assert {:done, hit} = thumb.(http, "HEAD")
      refute_received {:proxied, %{method: "GET", path: ^path}}, inspect(http)

      assert {wire_status(hit), wire_header(hit, "x-cache"), wire_header(hit, "content-length"), body(hit)} ==
               {[200], ["hit"], length, ""},
             inspect(http)
    end
  end

  # Download caches nothing over 100 MB.
  test "Scenario: A thumbnail HEAD for an image too big to cache answers the GET's 503 and fetches nothing", %{
    conn: conn,
    bucket: bucket,
    rules: rules
  } do
    limit = Application.fetch_env!(:slimserve, :thumb_max_file_bytes)
    on_exit(fn -> Application.put_env(:slimserve, :thumb_max_file_bytes, limit) end)
    Application.put_env(:slimserve, :thumb_max_file_bytes, 200 * 1024 * 1024)

    S3.put(bucket, "pic.png", "not read")
    path = "/#{bucket}/pic.png"
    big = Integer.to_string(101 * 1024 * 1024)

    Agent.update(rules, fn _rule ->
      fn request -> if request.method == "HEAD", do: {:header, "content-length", big}, else: :pass end
    end)

    head = head(conn, "/pic.png?thumb=1")
    refute_received {:proxied, %{method: "GET", path: ^path}}
    got = get(conn, "/pic.png?thumb=1")
    assert {head.status, got.status} == {503, 503}
    refute_received {:proxied, %{method: "GET", path: ^path}}
  end

  test "Scenario: A streamed answer with the wrong length, range or ETag is refused before any header", %{
    conn: conn,
    rules: rules
  } do
    fill_cache()

    for {name, value} <- [{"content-length", "5"}, {"content-range", "bytes 0-3/10"}, {"etag", ~s("other")}] do
      on_get(rules, {:header, name, value})
      conn = ranged(conn, "bytes=2-5")

      assert {conn.status, header(conn, "content-range")} == {502, nil},
             "#{name}: #{conn.status} #{inspect(conn.resp_body)}"

      assert {header(conn, "x-cache"), header(conn, "content-length")} == {nil, nil}, name
    end
  end

  @tag capture_log: true
  test "Scenario: A streamed body cut short closes an HTTP/1 connection and resets an HTTP/2 stream", %{rules: rules} do
    fill_cache()
    on_get(rules, {:truncate, 4})
    port = start_bandit()

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

  # On the wire, where Bandit, not the ConnTest adapter, decides what a HEAD's headers say.
  test "Scenario: A streamed HEAD sends GET's status and length over HTTP/1 and HTTP/2 but fetches no body", %{
    bucket: bucket
  } do
    path = "/#{bucket}/digits.txt"
    fill_cache()
    port = start_bandit()

    for http <- [Mint.HTTP1, Mint.HTTP2],
        {range, status, length} <- [{[], 200, "10"}, {[{"range", "bytes=2-5"}], 206, "4"}] do
      label = inspect({http, range})
      {:ok, mint} = http.connect(:http, "127.0.0.1", port)
      assert {:done, get} = fetch(mint, "GET", range)
      assert_received {:proxied, %{method: "GET", path: ^path}}
      assert {wire_status(get), wire_header(get, "content-length")} == {[status], [length]}, label

      {:ok, mint} = http.connect(:http, "127.0.0.1", port)
      assert {:done, head} = fetch(mint, "HEAD", range)
      refute_received {:proxied, %{method: "GET", path: ^path}}, label
      assert {wire_status(head), wire_header(head, "content-length"), body(head)} == {[status], [length], ""}, label
    end
  end

  defp start_bandit do
    server = start_supervised!({Bandit, plug: SlimServeWeb.Endpoint, ip: :loopback, port: 0, startup_log: false})
    {:ok, {_ip, port}} = ThousandIsland.listener_info(server)
    port
  end

  defp wire_status(responses), do: for({:status, _ref, status} <- responses, do: status)
  defp wire_header(responses, name), do: for({:headers, _ref, headers} <- responses, {^name, value} <- headers, do: value)

  # Within two seconds, so a hang that would end only at a timeout fails.
  defp fetch(conn, method \\ "GET", headers \\ [], path \\ "/digits.txt") do
    {:ok, conn, _ref} = Mint.HTTP.request(conn, method, path, headers, nil)
    collect(conn, [], System.monotonic_time(:millisecond) + 2_000)
  end

  # The `{:proxied, _}` ones stay in the mailbox for the test to check.
  defp collect(conn, acc, deadline) do
    receive do
      message when elem(message, 0) in [:tcp, :tcp_closed, :tcp_error] ->
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
