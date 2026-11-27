defmodule SlimServeWeb.ClientIpTest do
  # Not async: the login and the trusted proxies are application env.
  use SlimServeWeb.ConnCase, async: false

  setup %{conn: conn} do
    Application.put_env(:slimserve, :login, {"user", "pass"})
    Application.put_env(:slimserve, :trusted_proxies, [{{10, 0, 0, 0}, 8}])
    on_exit(fn -> for key <- [:login, :trusted_proxies], do: Application.delete_env(:slimserve, key) end)
    {:ok, conn: %{conn | remote_ip: {10, 1, 2, 3}}}
  end

  # A fresh address per call, so one test's failures never reach another's limit.
  defp ip do
    n = System.unique_integer([:positive])
    {198, 18 + rem(div(n, 65_536), 2), rem(div(n, 256), 256), rem(n, 256)}
  end

  defp text(ip), do: ip |> :inet.ntoa() |> List.to_string()

  defp forwarded(conn, lines), do: %{conn | req_headers: Enum.map(lines, &{"x-forwarded-for", &1}) ++ conn.req_headers}

  defp login(conn, password \\ "wrong"), do: post(conn, "/.app/login", %{"username" => "user", "password" => password})

  defp after_ten_failures(conn_for, last) do
    for _ <- 1..10, do: assert(conn_for.() |> login() |> response(401))
    login(last, "pass").status
  end

  test "Scenario: A spoofed X-Forwarded-For from an untrusted peer is ignored, and one from a trusted proxy is used", %{
    conn: proxy
  } do
    a = text(ip())
    assert after_ten_failures(fn -> forwarded(proxy, [a]) end, forwarded(proxy, [a])) == 429
    assert proxy |> forwarded([text(ip())]) |> login("pass") |> redirected_to(302) == "/"

    peer = %{proxy | remote_ip: ip()}
    assert after_ten_failures(fn -> forwarded(peer, [text(ip())]) end, forwarded(peer, [text(ip())])) == 429
  end

  test "an X-Forwarded-For entry that is not an address keeps the proxy as the client", %{conn: proxy} do
    proxy = %{proxy | remote_ip: {10, 9, 0, rem(System.unique_integer([:positive]), 256)}}

    assert after_ten_failures(fn -> forwarded(proxy, ["#{text(ip())}, not-an-address"]) end, forwarded(proxy, ["junk"])) ==
             429
  end

  test "an IPv4-mapped proxy address is trusted by its IPv4 range", %{conn: proxy} do
    a = ip()
    mapped = %{proxy | remote_ip: {0, 0, 0, 0, 0, 0xFFFF, 0x0A01, 0x0203}}
    assert after_ten_failures(fn -> forwarded(mapped, [text(a)]) end, %{proxy | remote_ip: a}) == 429
  end

  # HAProxy adds its own header line after one the client sent.
  test "a forged first X-Forwarded-For line does not hide the client the proxy added", %{conn: proxy} do
    a = text(ip())
    assert after_ten_failures(fn -> forwarded(proxy, [text(ip()), a]) end, forwarded(proxy, [text(ip()), a])) == 429
  end

  # Some proxies, such as Azure's, write the client's port, and brackets round an IPv6 address.
  test "an X-Forwarded-For entry with a port or brackets names its address", %{conn: proxy} do
    for {entry, client} <- [
          {"203.0.113.9:51234", {203, 0, 113, 9}},
          {"[2001:db8::1]:443", {0x2001, 0xDB8, 0, 0, 0, 0, 0, 1}},
          {"[2001:db8::1]", {0x2001, 0xDB8, 0, 0, 0, 0, 0, 1}},
          {"2001:db8::1", {0x2001, 0xDB8, 0, 0, 0, 0, 0, 1}}
        ] do
      assert SlimServeWeb.ClientIp.call(forwarded(proxy, [entry]), []).remote_ip == client
    end
  end
end
