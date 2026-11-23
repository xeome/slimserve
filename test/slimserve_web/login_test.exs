defmodule SlimServeWeb.LoginTest do
  # Not async: the login and the served root are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

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
    end)

    {:ok, conn: %{conn | remote_ip: ip()}}
  end

  # A fresh /64 each time, because the rate limit keys an IPv6 address by its /64.
  defp ip, do: {0xFD00, 0, 0, rem(System.unique_integer([:positive]), 65_536), 0, 0, 0, 1}

  defp wrong(conn), do: post(conn, "/.app/login", creds(%{"password" => "wrong"}))

  defp html(conn), do: put_req_header(conn, "accept", "text/html")

  defp creds(extra \\ %{}), do: Map.merge(%{"username" => "user", "password" => "pass"}, extra)

  # recycle/1 carries the response's cookies into the next request, as a browser would.
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
    test "every route answers the login page or a refusal until the user logs in", %{conn: conn} do
      for path <- ~w(/ /a.txt /sub/) do
        assert conn |> html() |> get(path) |> to_login?(), path
      end

      assert conn |> put_req_header("accept", "image/*") |> get("/a.png?thumb=1") |> response(401)
      assert conn |> get("/a.txt") |> response(401)
      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(html(conn), "/")
    end

    test "a refusal carries the content-security-policy header", %{conn: conn} do
      assert conn |> get("/a.txt") |> response(401)
      assert conn |> get("/a.txt") |> get_resp_header("content-security-policy") != []
    end

    test "after logging in, the same browser gets every route", %{conn: conn} do
      logged = log_in(conn)

      assert logged |> html() |> get("/") |> html_response(200)
      assert logged |> html() |> get("/sub/") |> html_response(200)
      assert logged |> html() |> get("/a.txt") |> response(200) == "alpha"
      assert {:ok, _view, _html} = live(logged, "/")
    end

    # The connected mount takes its session from the sent conn, so only the mount check can refuse it.
    test "a live socket whose session no longer matches the login is refused at mount", %{conn: conn} do
      page = conn |> log_in() |> html() |> get("/")
      assert html_response(page, 200)

      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert {:error, {:redirect, %{to: "/.app/login" <> _}}} = live(page)
    end

    test "a wrong password or a wrong username answers 401 and logs nobody in", %{conn: conn} do
      for bad <- [%{"password" => "wrong"}, %{"username" => "wrong"}] do
        posted = post(conn, "/.app/login", creds(bad))
        assert html_response(posted, 401)
        assert posted |> recycle() |> html() |> get("/") |> to_login?()
      end
    end

    test "a next that leaves the site redirects to /", %{conn: conn} do
      for next <- ["//evil.com", "https://evil.com", "/\\evil.com"] do
        assert conn |> post("/.app/login", creds(%{"next" => next})) |> redirected_to(302) == "/", next
      end
    end

    test "the login form returns the browser to the exact path and query it asked for", %{conn: conn} do
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

    test "logging out sends the browser back to the login page", %{conn: conn} do
      logged = log_in(conn)
      assert logged |> html() |> get("/") |> logout_button?()

      out = post(logged, "/.app/logout")
      assert to_login?(out)
      assert out |> recycle() |> html() |> get("/") |> to_login?()
    end

    test "a session whose password changed is logged out", %{conn: conn} do
      logged = log_in(conn)
      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert logged |> get("/a.txt") |> response(401)
      assert logged |> html() |> get("/") |> to_login?()
    end

    test "logging out from a session whose password changed reaches the login page", %{conn: conn} do
      logged = log_in(conn)
      Application.put_env(:slimserve, :login, {"user", "changed"})

      assert logged |> post("/.app/logout") |> to_login?()
    end

    test "the login page has no logout button", %{conn: conn} do
      refute conn |> html() |> get("/.app/login") |> logout_button?()
    end

    test "the 11th wrong attempt from one address answers 429, even with the right password", %{conn: conn} do
      for _ <- 1..10, do: assert(conn |> post("/.app/login", creds(%{"password" => "wrong"})) |> response(401))

      assert conn |> post("/.app/login", creds()) |> response(429)
      assert %{conn | remote_ip: ip()} |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    test "failures count against the whole /64 of an IPv6 address, and not another /64", %{conn: conn} do
      {a, b, c, d, _, _, _, _} = conn.remote_ip
      for _ <- 1..10, do: assert(conn |> wrong() |> response(401))

      assert %{conn | remote_ip: {a, b, c, d, 1, 2, 3, 4}} |> post("/.app/login", creds()) |> response(429)
      assert %{conn | remote_ip: ip()} |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    # The dual-stack listener shows an IPv4 client as ::ffff:a.b.c.d, all in one /64.
    test "an IPv4 client on the dual-stack listener is limited by its own address", %{conn: conn} do
      n = rem(System.unique_integer([:positive]), 65_536)
      mapped = %{conn | remote_ip: {0, 0, 0, 0, 0, 0xFFFF, 0x0A00, n}}
      for _ <- 1..10, do: assert(mapped |> wrong() |> response(401))

      other = %{conn | remote_ip: {0, 0, 0, 0, 0, 0xFFFF, 0x0A01, n}}
      assert other |> post("/.app/login", creds()) |> redirected_to(302) == "/"
    end

    test "30 parallel wrong attempts from one address get at most 10 answers of 401", %{conn: conn} do
      statuses =
        1..30
        |> Task.async_stream(fn _ -> wrong(conn).status end, max_concurrency: 30, timeout: :infinity)
        |> Enum.map(fn {:ok, status} -> status end)

      assert Enum.count(statuses, &(&1 == 401)) <= 10
      assert Enum.all?(statuses, &(&1 in [401, 429]))
    end
  end

  describe "with login unset" do
    setup do
      Application.delete_env(:slimserve, :login)
    end

    test "the login and logout paths answer 404 and pages have no logout button", %{conn: conn} do
      assert conn |> html() |> get("/.app/login") |> response(404)
      # ConnTest skips CSRF by default; a real browser's POST meets it.
      csrf = put_private(conn, :plug_skip_csrf_protection, false)
      assert csrf |> post("/.app/login", creds()) |> response(404)
      assert csrf |> post("/.app/logout") |> response(404)
      refute conn |> html() |> get("/") |> logout_button?()
    end
  end
end
