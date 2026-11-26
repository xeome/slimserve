defmodule SlimServeWeb.LoginTest do
  # Not async: the login and the served root are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias Phoenix.Socket.Broadcast

  @moduletag :tmp_dir

  # Each test logs in from its own address, so one test's failures never reach another's rate limit.
  setup %{conn: conn, tmp_dir: root} do
    File.write!(Path.join(root, "a.txt"), "alpha")
    File.write!(Path.join(root, "a&b%.txt"), "amp")
    File.mkdir_p!(Path.join(root, "sub"))
    Application.put_env(:slimserve, :root, root)
    Application.put_env(:slimserve, :login, {"user", "pass"})

    on_exit(fn ->
      Application.delete_env(:slimserve, :root)
      Application.delete_env(:slimserve, :login)
      Application.delete_env(:slimserve, :write_password)
    end)

    {:ok, conn: %{conn | remote_ip: ip()}}
  end

  # A fresh /64 each time, because the rate limit keys an IPv6 address by its /64.
  defp ip, do: {0xFD00, 0, 0, rem(System.unique_integer([:positive]), 65_536), 0, 0, 0, 1}

  defp wrong(conn), do: post(conn, "/.app/login", creds(%{"password" => "wrong"}))

  defp html(conn), do: put_req_header(conn, "accept", "text/html")

  defp creds(extra \\ %{}), do: Map.merge(%{"username" => "user", "password" => "pass"}, extra)

  defp log_in(conn) do
    posted = post(conn, "/.app/login", creds())
    assert redirected_to(posted, 302) == "/"
    recycle(posted)
  end

  defp to_login?(conn), do: conn |> redirected_to(302) |> URI.parse() |> Map.get(:path) == "/.app/login"

  defp logout_button?(conn) do
    conn |> html_response(200) |> LazyHTML.from_document() |> LazyHTML.query("#logout-form") |> Enum.count() == 1
  end

  describe "with login configured" do
    test "Scenario: Every route answers the login page or a refusal until the user logs in", %{conn: conn} do
      for path <- ~w(/ /a.txt /sub/) do
        assert conn |> html() |> get(path) |> to_login?(), path
      end

      assert conn |> put_req_header("accept", "image/*") |> get("/a.png?thumb=1") |> response(401)
      assert conn |> get("/a.txt") |> response(401)
      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(html(conn), "/")
    end

    test "Scenario: A refusal carries the content-security-policy header", %{conn: conn} do
      assert conn |> get("/a.txt") |> response(401)
      assert conn |> get("/a.txt") |> get_resp_header("content-security-policy") != []
    end

    test "Scenario: After logging in, the same browser gets every route", %{conn: conn} do
      logged = log_in(conn)

      assert logged |> html() |> get("/") |> html_response(200)
      assert logged |> html() |> get("/sub/") |> html_response(200)
      assert logged |> html() |> get("/a.txt") |> response(200) == "alpha"
      assert {:ok, _view, _html} = live(logged, "/")
    end

    # The connected mount takes its session from the sent conn, so only the mount check can refuse it.
    test "Scenario: A live socket whose session no longer matches the login is refused at mount", %{conn: conn} do
      page = conn |> log_in() |> html() |> get("/")
      assert html_response(page, 200)

      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(page)
    end

    test "Scenario: A wrong password or a wrong username answers 401 and logs nobody in", %{conn: conn} do
      for bad <- [%{"password" => "wrong"}, %{"username" => "wrong"}] do
        posted = post(conn, "/.app/login", creds(bad))
        assert html_response(posted, 401)
        assert posted |> recycle() |> html() |> get("/") |> to_login?()
      end
    end

    test "Scenario: A next that leaves the site redirects to /", %{conn: conn} do
      for next <- ["//evil.com", "https://evil.com", "/\\evil.com"] do
        assert conn |> post("/.app/login", creds(%{"next" => next})) |> redirected_to(302) == "/", next
      end
    end

    test "Scenario: The login form returns the browser to the exact path and query it asked for", %{conn: conn} do
      asked = "/a&b%25.txt?x=1"
      location = conn |> html() |> get(asked) |> redirected_to(302)

      [next] =
        conn
        |> html()
        |> get(location)
        |> html_response(200)
        |> LazyHTML.from_document()
        |> LazyHTML.query("#login-form input[name=next]")
        |> LazyHTML.attribute("value")

      landed = post(conn, "/.app/login", creds(%{"next" => next}))
      assert redirected_to(landed, 302) == asked
      assert landed |> recycle() |> get(asked) |> response(200) == "amp"
    end

    test "Scenario: Logging out sends the browser back to the login page", %{conn: conn} do
      logged = log_in(conn)
      assert logged |> html() |> get("/") |> logout_button?()

      out = post(logged, "/.app/logout")
      assert to_login?(out)
      assert out |> recycle() |> html() |> get("/") |> to_login?()
    end

    test "Scenario: A session whose password changed is logged out", %{conn: conn} do
      logged = log_in(conn)
      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert logged |> get("/a.txt") |> response(401)
      assert logged |> html() |> get("/") |> to_login?()
    end

    test "Scenario: Logging out from a session whose password changed reaches the login page", %{conn: conn} do
      logged = log_in(conn)
      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert logged |> post("/.app/logout") |> to_login?()
    end

    test "Scenario: The login page has no logout button", %{conn: conn} do
      refute conn |> html() |> get("/.app/login") |> logout_button?()
    end

    test "Scenario: The login page asks for the username once", %{conn: conn} do
      assert conn |> html() |> get("/.app/login") |> count("#login-form input[name=username]") == 1
    end

    test "Scenario: The 11th wrong viewer login from one address answers 429, even with the right password", %{conn: conn} do
      for _ <- 1..10, do: assert(conn |> post("/.app/login", creds(%{"password" => "wrong"})) |> response(401))

      assert conn |> post("/.app/login", creds()) |> response(429)
      assert %{conn | remote_ip: ip()} |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    test "Scenario: Failures count against the whole /64 of an IPv6 address, and not another /64", %{conn: conn} do
      {a, b, c, d, _, _, _, _} = conn.remote_ip
      for _ <- 1..10, do: assert(conn |> wrong() |> response(401))

      assert %{conn | remote_ip: {a, b, c, d, 1, 2, 3, 4}} |> post("/.app/login", creds()) |> response(429)
      assert %{conn | remote_ip: ip()} |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    # The dual-stack listener shows an IPv4 client as ::ffff:a.b.c.d, all in one /64.
    test "Scenario: An IPv4 client on the dual-stack listener is limited by its own address", %{conn: conn} do
      n = rem(System.unique_integer([:positive]), 65_536)
      mapped = %{conn | remote_ip: {0, 0, 0, 0, 0, 0xFFFF, 0x0A00, n}}
      for _ <- 1..10, do: assert(mapped |> wrong() |> response(401))

      other = %{conn | remote_ip: {0, 0, 0, 0, 0, 0xFFFF, 0x0A01, n}}
      assert other |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    test "Scenario: 30 parallel wrong attempts from one address get at most 10 answers of 401", %{conn: conn} do
      statuses =
        1..30
        |> Task.async_stream(fn _ -> wrong(conn).status end, max_concurrency: 30, timeout: :infinity)
        |> Enum.map(fn {:ok, status} -> status end)

      assert Enum.count(statuses, &(&1 == 401)) <= 10
      assert Enum.all?(statuses, &(&1 in [401, 429]))
    end

    test "Scenario: Logging out tells that browser's open viewer pages to disconnect", %{conn: conn} do
      logged = log_in(conn)
      socket_id = logged |> get("/") |> get_session("live_socket_id")
      assert is_binary(socket_id), "a viewer session names no socket, so logout can close none of its pages"
      SlimServeWeb.Endpoint.subscribe(socket_id)

      post(logged, "/.app/logout")
      assert_receive %Broadcast{topic: ^socket_id, event: "disconnect"}
    end
  end

  defp write_ui?(conn) do
    {:ok, view, _html} = live(conn, "/")
    has_element?(view, "#write-bar")
  end

  defp count(conn, selector) do
    conn |> html_response(200) |> LazyHTML.from_document() |> LazyHTML.query(selector) |> Enum.count()
  end

  describe "with a write password and login unset" do
    setup do
      Application.delete_env(:slimserve, :login)
      Application.put_env(:slimserve, :write_password, "wpass")
    end

    defp write_in(conn) do
      posted = post(conn, "/.app/login", %{"password" => "wpass"})
      assert redirected_to(posted, 302) == "/"
      recycle(posted)
    end

    test "Scenario: With login off, the login page asks only for the write password", %{conn: conn} do
      page = conn |> html() |> get("/.app/login")
      assert count(page, "#login-form input[name=username]") == 0
      assert count(page, "#login-form input[name=password]") == 1
    end

    test "Scenario: With login off, the listing links to the login page and shows no write UI until the user logs in", %{
      conn: conn
    } do
      assert conn |> html() |> get("/") |> count("#edit-login") == 1
      refute write_ui?(conn)
    end

    test "Scenario: Logging in with the write password shows the write UI and a logout button", %{conn: conn} do
      logged = write_in(conn)

      assert write_ui?(logged)
      page = logged |> html() |> get("/")
      assert count(page, "#logout-form") == 1
      assert count(page, "#edit-login") == 0
    end

    test "Scenario: A wrong write password answers 401 and grants no writes", %{conn: conn} do
      posted = post(conn, "/.app/login", %{"password" => "wrong"})
      assert html_response(posted, 401)
      refute posted |> recycle() |> write_ui?()
    end

    test "Scenario: The 11th wrong write login from one address answers 429, even with the right password", %{conn: conn} do
      for _ <- 1..10, do: assert(conn |> post("/.app/login", %{"password" => "wrong"}) |> response(401))

      assert conn |> post("/.app/login", %{"password" => "wpass"}) |> response(429)
    end

    test "Scenario: Changing the write password revokes the write session", %{conn: conn, tmp_dir: root} do
      logged = write_in(conn)
      Application.put_env(:slimserve, :write_password, "changed")

      {:ok, view, _html} = live(logged, "/")
      refute has_element?(view, "#write-bar")
      render_hook(view, "delete", %{"path" => "/a.txt"})
      assert File.exists?(Path.join(root, "a.txt"))
    end

    test "Scenario: Removing the write password revokes the write session", %{conn: conn} do
      logged = write_in(conn)
      Application.delete_env(:slimserve, :write_password)

      refute write_ui?(logged)
    end

    test "Scenario: A forged write session grants no writes", %{conn: conn, tmp_dir: root} do
      forged = init_test_session(conn, %{"write" => :crypto.strong_rand_bytes(32)})

      {:ok, view, _html} = live(forged, "/")
      refute has_element?(view, "#write-bar")
      render_hook(view, "delete", %{"path" => "/a.txt"})
      assert File.exists?(Path.join(root, "a.txt"))
    end

    # The socket's id is the session's live_socket_id (Phoenix.LiveView.Socket.id/1), and its
    # transport closes on a "disconnect" broadcast to it. LiveViewTest has no transport, so the
    # broadcast is what this sees.
    test "Scenario: Logging out tells that browser's open write pages to disconnect, and they remount without writes", %{
      conn: conn
    } do
      logged = write_in(conn)
      socket_id = logged |> get("/") |> get_session("live_socket_id")
      SlimServeWeb.Endpoint.subscribe(socket_id)

      out = post(logged, "/.app/logout")
      assert_receive %Broadcast{topic: ^socket_id, event: "disconnect"}
      refute out |> recycle() |> write_ui?()
    end

    # The pages opened after the first login carry its socket id, so a second login must keep it.
    test "Scenario: After a second write login in the same browser, logging out still reaches the first login's pages", %{
      conn: conn
    } do
      logged = write_in(conn)
      first = logged |> get("/") |> get_session("live_socket_id")
      again = logged |> post("/.app/login", %{"password" => "wpass"}) |> recycle()
      SlimServeWeb.Endpoint.subscribe(first)

      post(again, "/.app/logout")
      assert_receive %Broadcast{topic: ^first, event: "disconnect"}
    end
  end

  describe "with login and a write password" do
    setup do
      Application.put_env(:slimserve, :write_password, "wpass")
    end

    test "Scenario: The write password with the right username logs in with writes", %{conn: conn} do
      posted = post(conn, "/.app/login", creds(%{"password" => "wpass"}))
      assert redirected_to(posted, 302) == "/"

      logged = recycle(posted)
      assert logged |> html() |> get("/") |> html_response(200)
      assert write_ui?(logged)
    end

    test "Scenario: The write password with a wrong username is refused", %{conn: conn} do
      posted = post(conn, "/.app/login", %{"username" => "wrong", "password" => "wpass"})
      assert html_response(posted, 401)
      assert posted |> recycle() |> html() |> get("/") |> to_login?()
    end

    # The dashboard's socket meets only the write check, so that check holds the login too.
    test "Scenario: Changing the viewer password refuses the dashboard's live socket of a write session", %{conn: conn} do
      page =
        conn |> post("/.app/login", creds(%{"password" => "wpass"})) |> recycle() |> html() |> get("/.app/dashboard/home")

      assert html_response(page, 200)

      Application.put_env(:slimserve, :login, {"user", "changed"})
      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(page)
    end

    test "Scenario: The viewer password logs in without writes", %{conn: conn} do
      refute conn |> log_in() |> write_ui?()
    end

    test "Scenario: A viewer login after a write login in the same browser drops the writes", %{conn: conn} do
      writer = conn |> post("/.app/login", creds(%{"password" => "wpass"})) |> recycle()
      assert write_ui?(writer)

      viewer = writer |> post("/.app/login", creds()) |> recycle()
      refute write_ui?(viewer)
    end
  end

  describe "the dashboard" do
    setup do
      Application.delete_env(:slimserve, :login)
    end

    test "Scenario: The dashboard answers 404 with no write password, or an empty one", %{conn: conn} do
      assert conn |> html() |> get("/.app/dashboard") |> response(404)
      Application.put_env(:slimserve, :write_password, "")
      assert conn |> html() |> get("/.app/dashboard") |> response(404)
    end

    test "Scenario: The dashboard sends a browser without write rights to the login page, and answers anything else 401",
         %{conn: conn} do
      Application.put_env(:slimserve, :write_password, "wpass")

      assert conn |> html() |> get("/.app/dashboard") |> to_login?()
      assert conn |> get("/.app/dashboard") |> response(401)
      Application.put_env(:slimserve, :login, {"user", "pass"})
      assert conn |> log_in() |> html() |> get("/.app/dashboard") |> to_login?()
    end

    # The connected mount takes its session from the sent conn, so only the mount check can refuse it.
    test "Scenario: The dashboard answers a write session, and refuses its live socket once the password changed", %{
      conn: conn
    } do
      Application.put_env(:slimserve, :write_password, "wpass")
      logged = conn |> post("/.app/login", %{"password" => "wpass"}) |> recycle()

      page = logged |> html() |> get("/.app/dashboard/home")
      assert html_response(page, 200)

      Application.put_env(:slimserve, :write_password, "changed")
      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(page)
    end
  end

  describe "with login unset" do
    setup do
      Application.delete_env(:slimserve, :login)
    end

    test "Scenario: With login unset, the login and logout paths answer 404 and pages have no logout button", %{
      conn: conn
    } do
      assert conn |> html() |> get("/.app/login") |> response(404)
      # ConnTest skips CSRF by default; a real browser's POST meets it.
      csrf = put_private(conn, :plug_skip_csrf_protection, false)
      assert csrf |> post("/.app/login", creds()) |> response(404)
      assert csrf |> post("/.app/logout") |> response(404)
      refute conn |> html() |> get("/") |> logout_button?()
    end
  end
end
