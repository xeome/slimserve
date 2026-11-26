defmodule SlimServeWeb.HTTPSTest do
  # Not async: the HTTPS policy and login are application env.
  use SlimServeWeb.ConnCase, async: false

  @public_host "files.example.test"
  @public_origin "https://" <> @public_host
  @socket_path "/.app/live/websocket?vsn=2.0.0"

  @runtime_env ~w(
    PHX_SERVER PHX_HOST PORT SECRET_KEY_BASE DNS_CLUSTER_QUERY
    SLIMSERVE_HTTPS SLIMSERVE_ROOT SLIMSERVE_USERNAME SLIMSERVE_PASSWORD SLIMSERVE_WRITE_PASSWORD
    SLIMSERVE_UPLOAD_TYPES SLIMSERVE_UPLOAD_MAX_MB SLIMSERVE_CACHE_DIR
    SLIMSERVE_THUMB_MAX_DIM SLIMSERVE_THUMB_MAX_FILE_SIZE_MB SLIMSERVE_THUMB_CACHE_MB
  )

  setup do
    saved = for key <- [:ssl_redirect, :login], do: {key, Application.fetch_env(:slimserve, key)}

    on_exit(fn ->
      for {key, previous} <- saved do
        case previous do
          {:ok, value} -> Application.put_env(:slimserve, key, value)
          :error -> Application.delete_env(:slimserve, key)
        end
      end
    end)

    Application.delete_env(:slimserve, :ssl_redirect)
    Application.put_env(:slimserve, :login, {"user", "pass"})
  end

  test "Scenario: With HTTPS on, plain HTTP redirects to the canonical HTTPS host, the live socket path too", %{
    conn: conn
  } do
    enable_https()

    for path <- ["/folder/?sort=name", @socket_path] do
      response = get(conn, "http://other.example.test" <> path)
      assert %{status: 301, halted: true} = response
      assert get_resp_header(response, "location") == [@public_origin <> path]
    end
  end

  @tag capture_log: true
  test "Scenario: A forwarded HTTPS WebSocket origin passes the origin check", %{conn: conn} do
    enable_https()

    socket =
      %{conn | req_headers: [{"host", @public_host} | conn.req_headers]}
      |> put_req_header("x-forwarded-proto", "https")
      |> put_req_header("connection", "Upgrade")
      |> put_req_header("upgrade", "websocket")
      |> put_req_header("sec-websocket-version", "13")
      |> put_req_header("sec-websocket-key", "dGhlIHNhbXBsZSBub25jZQ==")

    url = "http://" <> @public_host <> @socket_path
    assert %{state: :upgraded} = socket |> put_req_header("origin", @public_origin) |> get(url)
    assert socket |> put_req_header("origin", "https://other.example.test") |> get(url) |> response(403)
  end

  test "Scenario: The login cookie follows the HTTPS policy even for localhost HTTP", %{conn: conn} do
    for request <- [conn, forwarded_https(conn)] do
      logged_in = log_in(request, "http://192.0.2.8:4081")
      assert logged_in.scheme == :http
      refute secure_cookie?(logged_in)
    end

    enable_https()

    assert conn |> forwarded_https() |> log_in("http://" <> @public_host) |> secure_cookie?()
    assert conn |> log_in("http://localhost:4081") |> secure_cookie?()
  end

  defp enable_https do
    saved = Map.new(@runtime_env, &{&1, System.get_env(&1)})

    ssl_redirect =
      try do
        Enum.each(@runtime_env, &System.delete_env/1)
        System.put_env(%{"SLIMSERVE_HTTPS" => "true", "PHX_HOST" => @public_host})

        "config/runtime.exs"
        |> Config.Reader.read!(env: :prod)
        |> get_in([:slimserve, :ssl_redirect])
      after
        for {name, value} <- saved do
          if value == nil, do: System.delete_env(name), else: System.put_env(name, value)
        end
      end

    Application.put_env(:slimserve, :ssl_redirect, ssl_redirect)
  end

  defp forwarded_https(conn), do: put_req_header(conn, "x-forwarded-proto", "https")

  defp log_in(conn, base_url) do
    response = post(conn, base_url <> "/.app/login", %{"username" => "user", "password" => "pass"})
    assert redirected_to(response, 302) == "/"
    response
  end

  defp secure_cookie?(conn) do
    [cookie] = get_resp_header(conn, "set-cookie")
    String.match?(cookie, ~r/;\s*secure(?:;|$)/i)
  end
end
