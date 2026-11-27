defmodule SlimServe.LoginClusterTest do
  # Not async: this node joins a cluster, and the login is application env.
  use SlimServeWeb.ConnCase, async: false

  alias SlimServe.Cluster

  @moduletag capture_log: true

  @peer_attempt [:slimserve, :login_limit, :peer_attempt]

  setup do
    Application.put_env(:slimserve, :login, {"user", "pass"})
    on_exit(fn -> Application.delete_env(:slimserve, :login) end)
    b = Cluster.start_peer()
    Cluster.watch(node(), @peer_attempt)
    Cluster.watch(b, @peer_attempt)
    {:ok, b: b}
  end

  # Each node hears the other's failures, so the 11th is limited wherever it lands.
  test "Scenario: Failed logins from one client on two nodes count toward one limit", %{conn: conn, b: b} do
    conn = %{conn | remote_ip: {198, 18, 0, rem(System.unique_integer([:positive]), 256)}}
    wrong = %{"username" => "user", "password" => "wrong"}

    for {on, heard_on} <- List.duplicate({node(), b}, 5) ++ List.duplicate({b, node()}, 5) do
      assert login(on, conn, wrong).status == 401
      assert_receive {:telemetry, ^heard_on, @peer_attempt, _metadata}
    end

    assert login(b, conn, %{"username" => "user", "password" => "pass"}).status == 429
  end

  defp login(node, conn, params),
    do: :erpc.call(node, Phoenix.ConnTest, :dispatch, [conn, SlimServeWeb.Endpoint, :post, "/.app/login", params])
end
