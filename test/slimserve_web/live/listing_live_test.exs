defmodule SlimServeWeb.ListingLiveTest do
  # Not async: the served root is application env, set per test.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.Root
  alias Vix.Vips.Image

  @moduletag :tmp_dir

  # The spec's bound: a connected listing updates within a second.
  @within 1_000

  setup %{tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    File.mkdir_p!(Path.join(root, "sub"))
    Application.put_env(:slimserve, :root, root)
    on_exit(fn -> Application.delete_env(:slimserve, :root) end)

    start_supervised!({SlimServe.Watcher, root: root})
    ready(root)
    {:ok, root: root}
  end

  # inotifywait reports nothing until its watches are set, so poke the root until an event comes back.
  defp ready(root, tries \\ 50) do
    :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, Root.topic(""))
    File.write!(Path.join(root, ".ready"), "x")

    receive do
      {:dir_changed, _dir} -> Phoenix.PubSub.unsubscribe(SlimServe.PubSub, Root.topic(""))
    after
      100 -> if tries > 0, do: ready(root, tries - 1), else: flunk("the watcher never reported an event")
    end
  end

  defp hrefs(view) do
    view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries .name a") |> LazyHTML.attribute("href")
  end

  defp assert_listed(view, expected, deadline \\ System.monotonic_time(:millisecond) + @within) do
    case hrefs(view) do
      ^expected ->
        :ok

      listed ->
        if System.monotonic_time(:millisecond) > deadline do
          flunk("listed #{inspect(listed)}, expected #{inspect(expected)}")
        end

        receive do
        after
          20 -> assert_listed(view, expected, deadline)
        end
    end
  end

  describe "Listing" do
    # A tab opened before the view toggle shipped reconnects with a session that has no "view".
    test "Scenario: A tab left open from before the view switch reconnects to the list view", %{conn: conn} do
      {:ok, view, _html} = live_isolated(conn, SlimServeWeb.ListingLive, session: %{"segments" => ["sub"]})

      assert has_element?(view, "#listing[data-view=list]")
      assert has_element?(view, "#view-list[aria-current]")
      refute has_element?(view, "#view-grid[aria-current]")
    end

    # The grid and list views share this markup and differ only in CSS.
    test "Scenario: A mixed folder shows each entry's thumbnail or kind icon, its size and its modified time", %{
      conn: conn,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "sub/dir"))
      for name <- ~w(a.txt b.txt .dot ignored.txt), do: File.write!(Path.join(root, "sub/dir/" <> name), "x")
      File.write!(Path.join(root, "sub/dir/.slimserveignore"), "ignored.txt\n")
      png = Path.join(root, "sub/pic.png")
      :ok = 4 |> Image.build_image!(4, [200, 80, 40]) |> Image.write_to_file(png)
      File.write!(Path.join(root, "sub/notes.txt"), "hello")

      for path <- [png, Path.join(root, "sub/notes.txt"), Path.join(root, "sub/dir")],
          do: File.touch!(path, 1_767_323_045)

      {:ok, view, _html} = live(conn, "/sub")

      rows =
        view
        |> render()
        |> LazyHTML.from_fragment()
        |> LazyHTML.query("#entries tr:not(#entries-empty)")
        |> Enum.map(fn row ->
          [href] = row |> LazyHTML.query(".name a") |> LazyHTML.attribute("href")
          [_name, _kind, size, _modified] = row |> LazyHTML.query("td") |> Enum.map(&String.trim(LazyHTML.text(&1)))
          time = LazyHTML.query(row, "time")

          icons =
            for kind <- ~w(img .hero-folder .hero-document), row |> LazyHTML.query(kind) |> Enum.count() > 0, do: kind

          {href, icons, size, {LazyHTML.attribute(time, "datetime"), LazyHTML.attribute(time, "title")}}
        end)

      stamp = {["2026-01-02T03:04:05Z"], ["2026-01-02 03:04 UTC"]}

      assert [
               {"/sub/dir", [".hero-folder"], "2 items", ^stamp},
               {"/sub/notes.txt", [".hero-document"], "5 B", ^stamp},
               {"/sub/pic.png", ["img"], pic_size, ^stamp}
             ] = rows

      assert pic_size == "#{File.stat!(png).size} B"
    end
  end

  describe "Live listing" do
    test "Scenario: A file added to and removed from a viewed directory shows and goes", %{conn: conn, root: root} do
      {:ok, view, _html} = live(conn, "/")
      assert hrefs(view) == ["/sub"]

      File.write!(Path.join(root, "new.txt"), "x")
      assert_listed(view, ["/sub", "/new.txt"])

      File.rm!(Path.join(root, "new.txt"))
      assert_listed(view, ["/sub"])
    end

    test "Scenario: A subdirectory's listing shows a file added to it", %{conn: conn, root: root} do
      {:ok, view, _html} = live(conn, "/sub")
      assert hrefs(view) == []

      File.write!(Path.join(root, "sub/new.txt"), "x")
      assert_listed(view, ["/sub/new.txt"])
    end

    # The listing re-reads the whole directory, so once the later file shows, the earlier two were passed over.
    test "Scenario: A dot-file and an ignored file never appear", %{conn: conn, root: root} do
      {:ok, view, _html} = live(conn, "/")

      File.write!(Path.join(root, ".hidden"), "x")
      File.write!(Path.join(root, ".slimserveignore"), "secret.txt\n")
      File.write!(Path.join(root, "secret.txt"), "x")
      File.write!(Path.join(root, "visible.txt"), "x")

      assert_listed(view, ["/sub", "/visible.txt"])
    end

    test "Scenario: A viewed directory that is removed sends the browser to its parent", %{conn: conn, root: root} do
      {:ok, view, _html} = live(conn, "/sub")

      File.rm_rf!(Path.join(root, "sub"))

      assert_redirect(view, "/", @within)
    end

    test "Scenario: A directory reached through a symlink shows a file added to its target", %{conn: conn, root: root} do
      File.ln_s!("sub", Path.join(root, "link"))
      {:ok, view, _html} = live(conn, "/link")

      File.write!(Path.join(root, "sub/new.txt"), "x")
      assert_listed(view, ["/link/new.txt"])
    end

    test "Scenario: A viewed directory whose parent is removed sends the browser to the nearest directory left", %{
      conn: conn,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "a/b"))
      {:ok, view, _html} = live(conn, "/a/b")

      File.rm_rf!(Path.join(root, "a"))

      assert_redirect(view, "/", @within)
    end
  end
end
