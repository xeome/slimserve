defmodule SlimServe.ShutdownTest do
  # Not async: this node joins a cluster.
  use ExUnit.Case, async: false

  alias SlimServe.Cluster

  @moduletag :tmp_dir
  @moduletag capture_log: true

  # Several times the 4 MB send-buffer ceiling, so the download is still in flight when the client stops reading.
  @size 32 * 1024 * 1024

  test "Scenario: After SIGTERM, /.app/readyz answers 503 while an open download completes", %{tmp_dir: root} do
    File.write!(Path.join(root, "big.bin"), :binary.copy("x", @size))
    endpoint = Application.fetch_env!(:slimserve, SlimServeWeb.Endpoint)
    http = Keyword.put(endpoint[:http], :port, 0)

    # The accept window waits for the test's :release, so no clock decides the order.
    b =
      Cluster.start_peer([
        {:root, root},
        {:shutdown_wait_ms, :infinity},
        {SlimServeWeb.Endpoint, Keyword.merge(endpoint, server: true, http: http)}
      ])

    {:ok, {_ip, port}} = :erpc.call(b, SlimServeWeb.Endpoint, :server_info, [:http])

    # A fixed receive buffer turns off loopback auto-tuning, which would take the whole file at once.
    {:ok, download} = :gen_tcp.connect(~c"127.0.0.1", port, [:binary, active: false, recbuf: 65_536])
    :ok = :gen_tcp.send(download, "GET /big.bin HTTP/1.1\r\nhost: localhost\r\nconnection: close\r\n\r\n")
    {:ok, first} = :gen_tcp.recv(download, 0)
    assert first =~ "HTTP/1.1 200"

    Cluster.watch(b, [:slimserve, :readiness, :draining])
    Node.monitor(b, true)
    os_pid = :erpc.call(b, :os, :getpid, [])
    {_out, 0} = System.cmd("kill", ["-TERM", List.to_string(os_pid)])
    assert_receive {:telemetry, ^b, [:slimserve, :readiness, :draining], _metadata}

    assert get(port, "/.app/readyz") =~ "HTTP/1.1 503"

    send({SlimServe.Readiness, b}, :release)
    response = first <> read_all(download)
    [_head, body] = :binary.split(response, "\r\n\r\n")
    assert byte_size(body) == @size
    assert_receive {:nodedown, ^b}
  end

  defp get(port, path) do
    {:ok, socket} = :gen_tcp.connect(~c"127.0.0.1", port, [:binary, active: false])
    :ok = :gen_tcp.send(socket, "GET #{path} HTTP/1.1\r\nhost: localhost\r\nconnection: close\r\n\r\n")
    read_all(socket)
  end

  defp read_all(socket, acc \\ []) do
    case :gen_tcp.recv(socket, 0) do
      {:ok, data} -> read_all(socket, [acc | data])
      {:error, :closed} -> IO.iodata_to_binary(acc)
    end
  end
end
