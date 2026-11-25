defmodule SlimServe.S3Fixture do
  @moduledoc """
  A fresh private bucket on the MinIO that `scripts/test-s3.sh` starts, and the app pointed at it.

  The helpers here talk to the bucket directly, not through the app, so a test can seed and inspect
  exact keys. A bucket that cannot be made raises, so `mix test --only s3` fails without MinIO.
  """
  import ExUnit.Callbacks

  alias SlimServe.Storage.S3.Metadata

  def endpoint, do: System.get_env("SLIMSERVE_TEST_S3_ENDPOINT", "http://127.0.0.1:9000")

  def config(bucket, endpoint \\ endpoint()) do
    [
      bucket: bucket,
      endpoint: endpoint,
      region: "us-east-1",
      access_key_id: System.get_env("SLIMSERVE_TEST_S3_KEY", "minioadmin"),
      secret_access_key: System.get_env("SLIMSERVE_TEST_S3_SECRET", "minioadmin")
    ]
  end

  def serve(opts \\ []) do
    bucket = "slimserve-#{System.unique_integer([:positive])}-#{System.os_time(:millisecond)}"
    %{status: 200} = raw(bucket, :put, "")

    if opts[:versioned] do
      body = ~s(<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>)
      %{status: 200} = raw(bucket, :put, "", params: [versioning: ""], body: body)
    end

    Application.put_env(:slimserve, :storage, :s3)
    Application.put_env(:slimserve, :s3, config(bucket, Keyword.get(opts, :endpoint, endpoint())))
    on_exit(fn -> for key <- [:storage, :s3, :s3_capabilities], do: Application.delete_env(:slimserve, key) end)

    cache_dir = Path.join(System.tmp_dir!(), bucket)
    on_exit(fn -> File.rm_rf!(cache_dir) end)

    start_supervised!(Metadata)

    start_supervised!(
      {SlimServe.DiskCache,
       name: SlimServe.DownloadCache, dir: cache_dir, max_bytes: Keyword.get(opts, :cache_bytes, 10_000_000), ext: "bin"}
    )

    bucket
  end

  @doc "Runs `fun` once, in the app's requesting process, right after its first `method` request for `key` returns."
  def after_request(method, key, fun) do
    id = {__MODULE__, make_ref()}

    handler = fn
      _event, _measurements, %{method: ^method, key: ^key}, _config ->
        :telemetry.detach(id)
        fun.()

      _event, _measurements, _metadata, _config ->
        :ok
    end

    :ok = :telemetry.attach(id, [:slimserve, :s3, :request], handler, nil)
    on_exit(fn -> :telemetry.detach(id) end)
  end

  def watch_requests do
    id = {__MODULE__, make_ref()}
    test = self()
    handler = fn _event, _measurements, meta, _config -> send(test, {:s3_request, meta.method, meta.key, meta.query}) end
    :ok = :telemetry.attach(id, [:slimserve, :s3, :request], handler, nil)
    on_exit(fn -> :telemetry.detach(id) end)
  end

  @doc """
  Opens `path` as a LiveView on this node and returns the view, which a test on another node can
  render. A process here holds it until `owner` exits.
  """
  def hold_view(path, owner) do
    caller = self()

    spawn(fn ->
      # LiveViewTest's client starts under the supervisor ExUnit keeps for a registered test process.
      ExUnit.OnExitHandler.register(self())
      ref = Process.monitor(owner)
      conn = Phoenix.ConnTest.dispatch(Phoenix.ConnTest.build_conn(), SlimServeWeb.Endpoint, :get, path, nil)
      send(caller, {:view, Phoenix.LiveViewTest.__live__(conn, path, [])})
      receive(do: ({:DOWN, ^ref, :process, _pid, _reason} -> :ok))
    end)

    receive(do: ({:view, result} -> result))
  end

  def put(bucket, key, body), do: %{status: 200} = raw(bucket, :put, key, body: body)

  def delete(bucket, key), do: %{status: 204} = raw(bucket, :delete, key)

  def get(bucket, key) do
    case raw(bucket, :get, key) do
      %{status: 200, body: body} -> body
      %{status: 404} -> nil
    end
  end

  def objects(bucket) do
    %{status: 200, body: body} = raw(bucket, :get, "", params: ["list-type": 2, "encoding-type": "url"])

    for %{"Key" => key} <- List.wrap(body["ListBucketResult"]["Contents"]), into: %{} do
      key = URI.decode_www_form(key)
      {key, get(bucket, key)}
    end
  end

  def versions(bucket, prefix) do
    %{status: 200, body: body} = raw(bucket, :get, "", params: [versions: "", prefix: prefix])
    result = body["ListVersionsResult"] || %{}

    for entry <- List.wrap(result["Version"]) ++ List.wrap(result["DeleteMarker"]),
        do: {entry["Key"], entry["VersionId"]}
  end

  @doc """
  A TCP proxy in front of MinIO; returns its endpoint. Each request, as `%{method, path, headers, body}`
  (path still URL-encoded, starting with the bucket), goes through `rule`, which answers:

    * `:pass` - forward it and relay the answer.
    * `:drop` - forward it, then close without an answer, as when a response is lost.
    * `{:truncate, n}` - forward it and relay only the answer's head and first `n` body bytes, then close.
    * `{:header, name, value}` - forward it and relay the answer with that header's value replaced.
    * `{:respond, status}` or `{:respond, status, body}` - answer without forwarding.
  """
  def proxy(rule) do
    {:ok, listen} = :gen_tcp.listen(0, [:binary, active: false, reuseaddr: true, ip: {127, 0, 0, 1}])
    {:ok, port} = :inet.port(listen)
    upstream = URI.parse(endpoint())
    start_supervised!(%{id: {:proxy, port}, start: {Task, :start_link, [fn -> accept(listen, upstream, rule) end]}})
    "http://127.0.0.1:#{port}"
  end

  defp accept(listen, upstream, rule) do
    {:ok, client} = :gen_tcp.accept(listen)
    {:ok, pid} = Task.start(fn -> receive(do: (:go -> relay(client, upstream, rule))) end)
    :ok = :gen_tcp.controlling_process(client, pid)
    send(pid, :go)
    accept(listen, upstream, rule)
  end

  # One request per upstream connection: the forwarded request asks MinIO to close after answering.
  defp relay(client, upstream, rule) do
    with {:ok, request} <- read_request(client, "") do
      verdict = rule.(request)
      answer = answer(verdict, request, upstream)
      [head, body] = :binary.split(answer, "\r\n\r\n")

      case verdict do
        :drop -> :ok
        {:truncate, n} -> :gen_tcp.send(client, [head, "\r\n\r\n", binary_part(body, 0, min(n, byte_size(body)))])
        {:header, name, value} -> :gen_tcp.send(client, [replace_header(head, name, value), "\r\n\r\n", body])
        _whole -> :gen_tcp.send(client, answer)
      end

      if verdict == :pass or match?({:header, _, _}, verdict), do: relay(client, upstream, rule)
    end

    :gen_tcp.close(client)
  end

  defp answer({:respond, status}, _request, _upstream), do: response(status, "")
  defp answer({:respond, status, body}, _request, _upstream), do: response(status, body)
  defp answer(_forward, request, upstream), do: forward(request, upstream)

  defp response(status, body),
    do:
      "HTTP/1.1 #{status} Stub\r\ncontent-length: #{byte_size(body)}\r\ncontent-type: application/xml\r\nconnection: close\r\n\r\n" <>
        body

  defp read_request(socket, buffer) do
    case :binary.split(buffer, "\r\n\r\n") do
      [head, rest] ->
        [line | lines] = String.split(head, "\r\n")
        [method, path, _version] = String.split(line, " ")
        headers = for l <- lines, [k, v] = String.split(l, ":", parts: 2), do: {String.downcase(k), String.trim(v)}
        length = headers |> List.keyfind("content-length", 0, {nil, "0"}) |> elem(1) |> String.to_integer()
        {:ok, body} = read_body(socket, rest, length)
        {:ok, %{method: method, path: path, headers: headers, body: body}}

      [_partial] ->
        with {:ok, data} <- :gen_tcp.recv(socket, 0), do: read_request(socket, buffer <> data)
    end
  end

  defp read_body(_socket, buffer, length) when byte_size(buffer) >= length, do: {:ok, buffer}

  defp read_body(socket, buffer, length) do
    {:ok, data} = :gen_tcp.recv(socket, 0)
    read_body(socket, buffer <> data, length)
  end

  defp forward(request, upstream) do
    {:ok, socket} = :gen_tcp.connect(String.to_charlist(upstream.host), upstream.port, [:binary, active: false])
    headers = for {k, v} <- request.headers, k != "connection", do: [k, ": ", v, "\r\n"]

    :ok =
      :gen_tcp.send(socket, [
        request.method,
        " ",
        request.path,
        " HTTP/1.1\r\n",
        headers,
        "connection: close\r\n\r\n",
        request.body
      ])

    read_all(socket, "")
  end

  defp read_all(socket, acc) do
    case :gen_tcp.recv(socket, 0) do
      {:ok, data} -> read_all(socket, acc <> data)
      {:error, :closed} -> acc
    end
  end

  defp replace_header(head, name, value) do
    head
    |> String.split("\r\n")
    |> Enum.map_join("\r\n", fn line ->
      if line |> String.downcase() |> String.starts_with?(name <> ":"), do: name <> ": " <> value, else: line
    end)
  end

  def raw(bucket, method, key, opts \\ []) do
    config = config(bucket)

    [
      method: method,
      url: "s3://#{bucket}/" <> URI.encode(key, &(&1 == ?/ or URI.char_unreserved?(&1))),
      retry: false,
      aws_sigv4: Keyword.take(config, [:access_key_id, :secret_access_key, :region])
    ]
    |> Req.new()
    |> ReqS3.attach(aws_endpoint_url_s3: config[:endpoint])
    |> Req.request!(opts)
    |> then(fn resp ->
      if is_binary(resp.body) and String.starts_with?(resp.body, "<?xml"),
        do: %{resp | body: ReqS3.XML.parse_s3(resp.body)},
        else: resp
    end)
  end
end
