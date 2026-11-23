defmodule SlimServeWeb.BrowseTest do
  # Not async: the served root is application env, set per test.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  @moduletag :tmp_dir

  # The canary sits one level above the root, so a traversal that works has something to fetch.
  setup %{tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    plant(tmp_dir, "canary.txt", "canary")
    File.mkdir_p!(Path.join(root, "a"))
    plant(root, "a.txt", "alpha")
    plant(root, "sub/.env", "secret")
    serve(root)
    {:ok, root: root}
  end

  defp serve(root) do
    Application.put_env(:slimserve, :root, root)
    on_exit(fn -> Application.delete_env(:slimserve, :root) end)
  end

  defp plant(dir, path, body) do
    file = Path.join(dir, path)
    File.mkdir_p!(Path.dirname(file))
    File.write!(file, body)
  end

  defp status(conn, path), do: get(conn, path).status

  defp ranged(conn, nil), do: get(conn, "/digits.txt")
  defp ranged(conn, range), do: conn |> put_req_header("range", range) |> get("/digits.txt")

  defp toggle(conn, path, id) do
    assert [href] =
             conn
             |> get(path)
             |> html_response(200)
             |> LazyHTML.from_document()
             |> LazyHTML.query("#" <> id)
             |> LazyHTML.attribute("href")

    get(conn, href)
  end

  # The listing's view and the ids of the toggle links marked current.
  defp view(conn) do
    page = conn |> html_response(200) |> LazyHTML.from_document()

    {page |> LazyHTML.query("#listing") |> LazyHTML.attribute("data-view"),
     page |> LazyHTML.query("#listing [aria-current]") |> LazyHTML.attribute("id")}
  end

  defp hrefs(conn, path) do
    {:ok, view, _html} = live(conn, path)
    view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries a") |> LazyHTML.attribute("href")
  end

  describe "Directory listing" do
    test "Scenario: The listing links exactly the entries a GET serves, folders first", %{
      conn: conn,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "Zdir"))
      plant(root, "b.txt", "b")
      plant(root, ".hidden", "x")
      plant(root, "skip.log", "x")
      plant(root, ".slimserveignore", "*.log\n")
      File.ln_s!("a.txt", Path.join(root, "link.txt"))
      File.ln_s!("../canary.txt", Path.join(root, "out.txt"))

      listed = hrefs(conn, "/")

      assert listed == ~w(/Zdir /a /sub /a.txt /b.txt /link.txt)
      assert Enum.sort(listed) == Enum.sort(for name <- File.ls!(root), status(conn, "/" <> name) == 200, do: "/" <> name)
    end

    test "Scenario: A listed name with reserved characters downloads through its link", %{conn: conn, root: root} do
      plant(root, "sub/a #1%.txt", "one")
      plant(root, "sub/é.txt", "two")

      assert for(href <- hrefs(conn, "/sub"), do: response(get(conn, href), 200)) == ["one", "two"]
    end

    test "a fifo is neither served nor listed", %{conn: conn, root: root} do
      {_out, 0} = System.cmd("mkfifo", [Path.join(root, "pipe")])

      assert status(conn, "/pipe") == 404
      refute "/pipe" in hrefs(conn, "/")
    end

    test "the page's icon link downloads the app's own favicon", %{conn: conn} do
      [href] =
        conn
        |> get("/")
        |> html_response(200)
        |> LazyHTML.from_document()
        |> LazyHTML.query("link[rel=icon]")
        |> LazyHTML.attribute("href")

      assert conn |> get(href) |> response(200) == File.read!(Application.app_dir(:slimserve, "priv/static/favicon.ico"))
    end

    test "the listing carries the app CSP without the dashboard's nonce", %{conn: conn} do
      [csp] = conn |> get("/") |> get_resp_header("content-security-policy")

      assert csp =~ "base-uri 'self'"
      assert csp =~ "frame-ancestors 'self'"
      refute csp =~ "nonce-"
    end
  end

  describe "Listing views" do
    test "Scenario: The Grid link keeps grid in a cookie for a year and returns to the folder", %{
      conn: conn,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "a b#"))

      for path <- ["/sub", "/a%20b%23"] do
        toggled = toggle(conn, path, "view-grid")

        assert redirected_to(toggled, 302) == path
        assert %{value: "grid", max_age: max_age} = toggled.resp_cookies["slimserve-view"]
        assert max_age >= 31_536_000
      end
    end

    test "Scenario: After the Grid link, a reload renders the grid view", %{conn: conn} do
      toggled = toggle(conn, "/sub", "view-grid")

      # recycle/1 carries the response's cookies into the next request, as a browser would.
      assert toggled |> recycle() |> get("/sub") |> view() == {["grid"], ["view-grid"]}
    end

    test "Scenario: From the grid, the List link keeps list in the cookie and a reload renders the list view", %{
      conn: conn
    } do
      grid = put_req_cookie(conn, "slimserve-view", "grid")
      assert grid |> get("/sub") |> view() == {["grid"], ["view-grid"]}

      toggled = toggle(grid, "/sub", "view-list")

      assert redirected_to(toggled, 302) == "/sub"
      assert %{value: "list"} = toggled.resp_cookies["slimserve-view"]
      assert toggled |> recycle() |> get("/sub") |> view() == {["list"], ["view-list"]}
    end

    test "Scenario: With no cookie the folder renders the list view, and a bogus view sets none", %{conn: conn} do
      assert conn |> get("/sub") |> view() == {["list"], ["view-list"]}

      bogus = get(conn, "/sub?view=bogus")
      assert view(bogus) == {["list"], ["view-list"]}
      refute Map.has_key?(bogus.resp_cookies, "slimserve-view")
    end
  end

  describe "File download" do
    test "Scenario: A file downloads with its bytes and MIME type", %{conn: conn, root: root} do
      plant(root, "page.html", "<p>hi</p>")

      conn = get(conn, "/a.txt")
      assert response(conn, 200) == "alpha"
      assert get_resp_header(conn, "content-type") == ["text/plain"]
      assert get_resp_header(conn, "x-content-type-options") == ["nosniff"]

      assert get_resp_header(conn, "content-security-policy") == [
               "default-src 'self'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'self'"
             ]

      assert conn |> get("/page.html") |> get_resp_header("content-type") == ["text/html"]
    end

    test "Scenario: A download ignores the Accept header", %{conn: conn} do
      conn = conn |> put_req_header("accept", "application/json") |> get("/a.txt")

      assert response(conn, 200) == "alpha"
    end

    test "Scenario: A symlink downloads with the MIME type of its own name", %{conn: conn, root: root} do
      plant(root, "x.html", "<p>hi</p>")
      File.ln_s!("x.html", Path.join(root, "x.txt"))

      assert conn |> get("/x.txt") |> get_resp_header("content-type") == ["text/plain"]
    end

    test "Scenario: A file that cannot be read answers 404 and is not listed", %{conn: conn, root: root} do
      plant(root, "locked.txt", "x")
      File.chmod!(Path.join(root, "locked.txt"), 0o000)

      refute "/locked.txt" in hrefs(conn, "/")
      assert status(conn, "/locked.txt") == 404
    end

    test "Scenario: A served file is not shadowed by the app's own static files", %{conn: conn, root: root} do
      plant(root, "favicon.ico", "mine")

      assert conn |> get("/favicon.ico") |> response(200) == "mine"
    end
  end

  describe "Range requests" do
    setup %{root: root} do
      plant(root, "digits.txt", "0123456789")
    end

    test "Scenario: A single range answers 206 with exactly its bytes", %{conn: conn} do
      for {range, body, content_range} <- [
            {"bytes=2-5", "2345", "bytes 2-5/10"},
            {"bytes=7-", "789", "bytes 7-9/10"},
            {"bytes=-3", "789", "bytes 7-9/10"},
            {"bytes=8-99", "89", "bytes 8-9/10"},
            {"bytes=-50", "0123456789", "bytes 0-9/10"}
          ] do
        conn = ranged(conn, range)
        assert response(conn, 206) == body, range
        assert get_resp_header(conn, "content-range") == [content_range], range
        assert get_resp_header(conn, "accept-ranges") == ["bytes"], range
      end
    end

    test "Scenario: A range past the end answers 416 with the size", %{conn: conn} do
      for range <- ~w(bytes=10- bytes=20-30 bytes=-0) do
        conn = ranged(conn, range)
        assert conn.status == 416, range
        assert get_resp_header(conn, "content-range") == ["bytes */10"], range
        assert get_resp_header(conn, "accept-ranges") == ["bytes"], range
      end
    end

    test "Scenario: No range, several ranges or a malformed one answers the full 200", %{conn: conn} do
      for range <- [nil, "bytes=0-1,4-5", "bytes=abc", "bytes=5-2", "items=0-1"] do
        conn = ranged(conn, range)
        assert response(conn, 200) == "0123456789", inspect(range)
        assert get_resp_header(conn, "accept-ranges") == ["bytes"], inspect(range)
        assert get_resp_header(conn, "content-range") == [], inspect(range)
      end
    end

    # put_req_header replaces, so the second header goes on the list by hand.
    test "Scenario: Two Range headers answer the full 200", %{conn: conn} do
      conn = %{conn | req_headers: [{"range", "bytes=0-1"}, {"range", "bytes=2-3"} | conn.req_headers]}

      assert conn |> get("/digits.txt") |> response(200) == "0123456789"
    end

    test "Scenario: An empty file answers the full 200 whatever the range", %{conn: conn, root: root} do
      plant(root, "empty.txt", "")

      assert conn |> put_req_header("range", "bytes=0-") |> get("/empty.txt") |> response(200) == ""
    end
  end

  test "browser refusals share the same 404 shell without identifying the path", %{conn: conn} do
    browser = put_req_header(conn, "accept", "text/html,application/xhtml+xml")

    for path <- ["/missing.txt", "/sub/.env", "/a.txt?thumb=1"] do
      document = browser |> get(path) |> html_response(404) |> LazyHTML.from_document()
      assert ["/"] == LazyHTML.attribute(LazyHTML.query(document, "#app-shell header a"), "href")
      assert "Page not found" == LazyHTML.text(LazyHTML.query(document, "#not-found h1"))
      refute LazyHTML.text(document) =~ path
    end

    assert conn |> put_req_header("accept", "application/json") |> get("/sub/.env") |> response(404) == "Not Found"
  end

  test "disabled app routes have the same refusal contract as unavailable files", %{conn: conn} do
    browser = put_req_header(conn, "accept", "text/html,application/xhtml+xml")
    plain = put_req_header(conn, "accept", "application/json")

    paths = [
      "/missing.txt",
      "/sub/.env",
      "/a.txt?thumb=1",
      "/.app/login",
      "/.app/dashboard"
    ]

    for {method, path} <- Enum.map(paths, &{:get, &1}) ++ [{:post, "/.app/login"}, {:post, "/.app/logout"}] do
      refused = dispatch(browser, SlimServeWeb.Endpoint, method, path)
      document = refused |> html_response(404) |> LazyHTML.from_document()

      assert "Page not found" == LazyHTML.text(LazyHTML.query(document, "#not-found h1"))
      assert "Guest" == document |> LazyHTML.query(".session-label") |> LazyHTML.text() |> String.trim()
      assert ["/"] == LazyHTML.attribute(LazyHTML.query(document, "#not-found a"), "href")
      assert [] == LazyHTML.attribute(LazyHTML.query(document, "#edit-login, #logout-form"), "id")

      assert ["default-src 'self'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'self'"] ==
               get_resp_header(refused, "content-security-policy")

      assert plain |> dispatch(SlimServeWeb.Endpoint, method, path) |> response(404) == "Not Found"
    end
  end

  describe "Containment" do
    test "Scenario: A path with .. segments answers 404", %{conn: conn} do
      for path <- ~w(/../etc/passwd /../canary.txt /a/../../canary.txt) do
        assert status(conn, path) == 404, path
      end
    end

    test "Scenario: An encoded slash answers 404", %{conn: conn} do
      for path <- ~w(/a%2F..%2F..%2Fcanary.txt /sub%2F.env) do
        assert status(conn, path) == 404, path
      end
    end

    test "Scenario: A NUL byte in a path answers 404", %{conn: conn} do
      assert status(conn, "/a.txt%00.html") == 404
    end

    test "Scenario: A name that is not valid UTF-8 answers 404", %{conn: conn, root: root} do
      plant(root, <<0xFF, ".txt">>, "x")
      assert status(conn, "/%FF.txt") == 404

      # With an ignore file above it, the name reaches the pattern match.
      plant(root, ".slimserveignore", "*.log\n")
      assert status(conn, "/%FF.txt") == 404
    end

    test "Scenario: A symlink that leaves the root answers 404", %{conn: conn, root: root, tmp_dir: tmp_dir} do
      plant(tmp_dir, "outside/o.txt", "outside")
      File.ln_s!("../canary.txt", Path.join(root, "climb.txt"))
      File.ln_s!("/etc", Path.join(root, "e"))
      File.ln_s!("../outside", Path.join(root, "out"))

      for path <- ~w(/climb.txt /e /e/passwd /out /out/o.txt) do
        assert status(conn, path) == 404, path
      end
    end

    test "Scenario: A relative symlink that stays inside the root is served", %{conn: conn, root: root} do
      plant(root, "sub/x.txt", "x")
      File.ln_s!("a.txt", Path.join(root, "link.txt"))
      File.ln_s!("sub", Path.join(root, "sublink"))

      assert conn |> get("/link.txt") |> response(200) == "alpha"
      assert conn |> get("/sublink/x.txt") |> response(200) == "x"
    end

    test "Scenario: The canary downloads when its own directory is the root", %{conn: conn, tmp_dir: tmp_dir} do
      serve(tmp_dir)

      assert conn |> get("/canary.txt") |> response(200) == "canary"
    end
  end

  describe "Dot-files" do
    test "Scenario: A path with a dot component answers 404", %{conn: conn, root: root} do
      plant(root, ".env", "secret")
      plant(root, ".git/config", "secret")

      for path <- ~w(/.env /.git /.git/config /sub/.env) do
        assert status(conn, path) == 404, path
      end
    end

    test "Scenario: A symlink to a dot-file answers 404", %{conn: conn, root: root} do
      plant(root, ".git/config", "secret")
      File.ln_s!("sub/.env", Path.join(root, "pub"))
      File.ln_s!(".git", Path.join(root, "g"))

      for path <- ~w(/pub /g /g/config) do
        assert status(conn, path) == 404, path
      end
    end
  end

  describe "Ignore files" do
    test "Scenario: A nested ignore file hides a file below it only", %{conn: conn, root: root} do
      plant(root, "sub/.slimserveignore", "secret.txt\n")
      plant(root, "sub/secret.txt", "s")
      plant(root, "sub/deep/secret.txt", "s")
      plant(root, "sub/open.txt", "open")
      plant(root, "secret.txt", "mine")

      assert status(conn, "/sub/secret.txt") == 404
      assert status(conn, "/sub/deep/secret.txt") == 404
      assert conn |> get("/sub/open.txt") |> response(200) == "open"
      assert conn |> get("/secret.txt") |> response(200) == "mine"
    end

    test "Scenario: An ignored directory hides everything under it", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", "private/\n")
      plant(root, "private/x.txt", "x")
      plant(root, "sub/private", "a file, and the pattern names a directory")

      assert status(conn, "/private") == 404
      assert status(conn, "/private/x.txt") == 404
      assert status(conn, "/sub/private") == 200
    end

    test "Scenario: A negated pattern serves the file it names", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", "*.txt\n!keep.txt\n")
      plant(root, "keep.txt", "kept")

      assert status(conn, "/a.txt") == 404
      assert conn |> get("/keep.txt") |> response(200) == "kept"
    end

    test "Scenario: A negated pattern cannot serve a file under an ignored directory", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", "private/\n!private/keep.txt\n")
      plant(root, "private/keep.txt", "kept")

      assert status(conn, "/private/keep.txt") == 404
    end

    test "Scenario: A deeper ignore file has the last word", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", "*.txt\n")
      plant(root, "sub/.slimserveignore", "!keep.txt\n")
      plant(root, "sub/keep.txt", "kept")
      plant(root, "sub/other.txt", "o")

      assert conn |> get("/sub/keep.txt") |> response(200) == "kept"
      assert status(conn, "/sub/other.txt") == 404
    end

    test "Scenario: A symlink into an ignored directory answers 404", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", "private/\n")
      plant(root, "private/x.txt", "x")
      File.ln_s!("private/x.txt", Path.join(root, "px.txt"))
      File.ln_s!("private", Path.join(root, "pd"))

      for path <- ~w(/px.txt /pd /pd/x.txt) do
        assert status(conn, path) == 404, path
      end
    end

    test "Scenario: An ignore file that is not valid UTF-8 hides its whole directory", %{conn: conn, root: root} do
      plant(root, "bad/.slimserveignore", <<0xFF, "\n">>)
      plant(root, "bad/x.txt", "x")

      assert status(conn, "/bad/x.txt") == 404
      assert status(conn, "/bad") == 404
    end

    test "Scenario: An ignore file that starts with a byte-order mark still applies", %{conn: conn, root: root} do
      plant(root, ".slimserveignore", <<0xEF, 0xBB, 0xBF>> <> "secret.txt\n")
      plant(root, "secret.txt", "s")

      assert status(conn, "/secret.txt") == 404
      refute "/secret.txt" in hrefs(conn, "/")
    end

    test "Scenario: An unreadable ignore file hides its whole directory", %{conn: conn, root: root} do
      plant(root, "locked/.slimserveignore", "")
      plant(root, "locked/x.txt", "x")
      File.chmod!(Path.join(root, "locked/.slimserveignore"), 0o000)

      assert status(conn, "/locked/x.txt") == 404
      assert status(conn, "/locked") == 404
    end
  end
end
