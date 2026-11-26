defmodule SlimServeWeb.ListingUITest do
  # Not async: the served root and the write password are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.Root
  alias Vix.Vips.Image
  alias Vix.Vips.MutableImage

  @moduletag :tmp_dir

  # The spec's bound: a connected listing updates within a second.
  @within 1_000

  setup %{conn: conn, tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    File.mkdir_p!(Path.join(root, "sub"))
    Application.put_env(:slimserve, :root, root)
    Application.put_env(:slimserve, :write_password, "wpass")
    on_exit(fn -> for key <- [:root, :write_password], do: Application.delete_env(:slimserve, key) end)

    start_supervised!({SlimServe.Watcher, root: root})
    ready(root)

    # A fresh /64 per test, so no test's attempts reach another's rate limit.
    conn = %{conn | remote_ip: {0xFD03, 0, 0, rem(System.unique_integer([:positive]), 65_536), 0, 0, 0, 1}}
    writer = conn |> post("/.app/login", %{"password" => "wpass"}) |> recycle()
    {:ok, root: root, reader: conn, writer: writer}
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

  defp eventually(check, deadline \\ System.monotonic_time(:millisecond) + @within) do
    cond do
      check.() ->
        :ok

      System.monotonic_time(:millisecond) > deadline ->
        flunk("the page never reached the expected state")

      true ->
        receive do
        after
          20 -> eventually(check, deadline)
        end
    end
  end

  defp doc(view), do: view |> render() |> LazyHTML.from_fragment()

  defp names(view), do: view |> doc() |> LazyHTML.query("#entries .name .label") |> Enum.map(&LazyHTML.text/1)

  defp text(node), do: node |> LazyHTML.text() |> String.trim()

  defp tree(view) do
    for node <- view |> doc() |> LazyHTML.query("#tree .node") do
      [path] = LazyHTML.attribute(node, "data-path")

      {path, node |> LazyHTML.query(".ct") |> text(), LazyHTML.attribute(node, "aria-expanded") == ["true"],
       node |> LazyHTML.query("[aria-current=page]") |> Enum.count() == 1}
    end
  end

  defp card(view) do
    for cat <- view |> doc() |> LazyHTML.query("#card .cat"), into: %{} do
      [kind] = LazyHTML.attribute(cat, "data-kind")
      {kind, {cat |> LazyHTML.query(".n") |> text(), cat |> LazyHTML.query(".size") |> text()}}
    end
  end

  defp select(view, name), do: view |> element("#entries tr[data-name='#{name}']") |> render_click()

  defp filter(view, text), do: view |> element("#filter-form") |> render_change(%{"filter" => text})

  defp selected(view),
    do: view |> doc() |> LazyHTML.query("#entries tr[aria-selected=true]") |> LazyHTML.attribute("data-name")

  defp disk(root) do
    for path <- Path.wildcard(Path.join(root, "**"), match_dot: true), into: %{} do
      {Path.relative_to(path, root), if(File.dir?(path), do: :dir, else: File.read!(path))}
    end
  end

  describe "folder tree" do
    test "Scenario: The folder tree shows each ancestor's folders with exact counts and the open path expanded", %{
      reader: reader,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "a/b/d"))
      File.mkdir_p!(Path.join(root, "a/c"))
      File.mkdir_p!(Path.join(root, "z"))
      File.write!(Path.join(root, "f.txt"), "f")
      File.write!(Path.join(root, "a/b/q.txt"), "q")
      for name <- ~w(x.txt y.txt .dot ignored.txt), do: File.write!(Path.join(root, "a/b/d/" <> name), "x")
      File.write!(Path.join(root, "a/b/d/.slimserveignore"), "ignored.txt\n")

      {:ok, view, _html} = live(reader, "/a/b")

      assert tree(view) == [
               {"a", "2", true, false},
               {"a/b", "2", true, true},
               {"a/b/d", "2", false, false},
               {"a/c", "0", false, false},
               {"sub", "0", false, false},
               {"z", "0", false, false}
             ]

      File.mkdir!(Path.join(root, "a/b/new"))
      eventually(fn -> {"a/b/new", "0", false, false} in tree(view) end)
    end

    test "Scenario: A guest unfolds a folder off the open path in place, and it stays unfolded across a relist and an open",
         %{
           reader: reader,
           root: root
         } do
      File.mkdir_p!(Path.join(root, "z/y/x"))
      File.mkdir_p!(Path.join(root, "z/w"))
      File.write!(Path.join(root, "z/f.txt"), "f")
      {:ok, view, _html} = live(reader, "/")
      chev = "#tree .node[data-path='z'] button.chev"

      assert has_element?(view, chev <> "[aria-expanded=false]")
      view |> element(chev) |> render_click()

      assert tree(view) == [
               {"sub", "0", false, false},
               {"z", "3", true, false},
               {"z/w", "0", false, false},
               {"z/y", "1", false, false}
             ]

      assert has_element?(view, chev <> "[aria-expanded=true]")
      assert has_element?(view, "#tree .node[data-path='z/y'] button.chev[aria-expanded=false]")
      assert has_element?(view, "#entries tr[data-name='z']")
      assert page_title(view) == "/"

      File.write!(Path.join(root, "new.txt"), "n")
      eventually(fn -> "new.txt" in names(view) end)
      assert {"z/y", "1", false, false} in tree(view)

      render_hook(view, "open", %{"path" => "/sub"})
      assert_reply(view, %{ok: true})
      assert {"z/y", "1", false, false} in tree(view)
    end

    test "Scenario: An unfolded level drops a deleted subfolder and shows a renamed one, with no click", %{
      reader: reader,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "z/y/x"))
      File.mkdir_p!(Path.join(root, "z/w"))
      {:ok, view, _html} = live(reader, "/")
      view |> element("#tree .node[data-path='z'] button.chev") |> render_click()
      assert {"z/y", "1", false, false} in tree(view)

      File.rm_rf!(Path.join(root, "z/y"))
      File.rename!(Path.join(root, "z/w"), Path.join(root, "z/v"))

      eventually(fn ->
        tree(view) == [{"sub", "0", false, false}, {"z", "1", true, false}, {"z/v", "0", false, false}]
      end)
    end

    test "Scenario: A file added to a folder shown as a node updates that node's count", %{reader: reader, root: root} do
      File.mkdir_p!(Path.join(root, "z"))
      {:ok, view, _html} = live(reader, "/sub")
      assert {"z", "0", false, false} in tree(view)

      File.write!(Path.join(root, "z/new.txt"), "n")
      eventually(fn -> {"z", "1", false, false} in tree(view) end)
    end

    test "Scenario: After an open away from a level, only changes in the levels the page still shows reach it", %{
      reader: reader,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "z/y/x"))
      {:ok, view, _html} = live(reader, "/z/y")
      dirs = fn -> Enum.sort(for "dir:" <> dir <- Registry.keys(SlimServe.PubSub, view.pid), do: dir) end
      assert dirs.() == ["", "sub", "z", "z/y", "z/y/x"]

      render_hook(view, "open", %{"path" => "/sub"})
      assert_reply(view, %{ok: true})
      assert dirs.() == ["", "sub", "z"]
    end

    test "Scenario: An unfolded folder keeps the levels above it across an open, unfolded and live", %{
      reader: reader,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "z/q"))
      File.mkdir_p!(Path.join(root, "z/y/x"))
      {:ok, view, _html} = live(reader, "/z/q")
      dirs = fn -> Enum.sort(for "dir:" <> dir <- Registry.keys(SlimServe.PubSub, view.pid), do: dir) end
      view |> element("#tree .node[data-path='z/y'] button.chev") |> render_click()

      render_hook(view, "open", %{"path" => "/sub"})
      assert_reply(view, %{ok: true})
      assert dirs.() == ["", "sub", "z", "z/q", "z/y", "z/y/x"]
      assert has_element?(view, "#tree .node[data-path='z'] button.chev[aria-expanded=true]")
      assert has_element?(view, "#tree .node[data-path='z/y/x']")
    end

    test "Scenario: A symlinked folder's node follows the link to its new target", %{reader: reader, root: root} do
      File.mkdir_p!(Path.join(root, "t1"))
      File.mkdir_p!(Path.join(root, "t2"))
      File.write!(Path.join(root, "t1/a.txt"), "a")
      for name <- ~w(b.txt c.txt), do: File.write!(Path.join(root, "t2/" <> name), "x")
      File.ln_s!("t1", Path.join(root, "l"))
      {:ok, view, _html} = live(reader, "/sub")
      assert {"l", "1", false, false} in tree(view)

      File.rm!(Path.join(root, "l"))
      File.ln_s!("t2", Path.join(root, "l"))
      eventually(fn -> {"l", "2", false, false} in tree(view) end)

      File.write!(Path.join(root, "t2/d.txt"), "d")
      eventually(fn -> {"l", "3", false, false} in tree(view) end)
    end

    test "Scenario: Unfolding a hidden, ignored, .. or file path is refused, the tree unchanged", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.mkdir_p!(Path.join(root, ".hidden/in"))
      File.mkdir_p!(Path.join(root, "ign/in"))
      File.write!(Path.join(root, ".slimserveignore"), "ign\n")
      File.mkdir_p!(Path.join(root, "sub/inner"))
      {:ok, view, _html} = live(reader, "/sub")
      before = render(view)

      for path <- ~w(/a.txt /nope /.hidden /ign /sub/.. /sub/../ign /sub/%2E%2E /sub%2F..%2Fign /%zz) do
        render_hook(view, "tree_expand", %{"path" => path})
        assert_reply(view, %{ok: false})
        assert render(view) == before, "#{path} changed the page"
      end
    end

    test "Scenario: The rail cookie renders the rail folded on the first paint", %{conn: conn} do
      side = fn conn -> conn |> get("/") |> html_response(200) |> LazyHTML.from_document() |> LazyHTML.query("#side") end
      classes = fn conn -> conn |> side.() |> LazyHTML.attribute("class") |> hd() |> String.split() end

      assert "rail" in classes.(put_req_cookie(conn, "slimserve-rail", "rail"))
      refute "rail" in classes.(put_req_cookie(conn, "slimserve-rail", "open"))
      refute "rail-auto" in classes.(put_req_cookie(conn, "slimserve-rail", "open"))
      # No cookie: the CSS folds it at phone width, where the hook then makes it explicit.
      assert "rail-auto" in classes.(conn)
      refute "rail" in classes.(conn)
    end

    test "Scenario: The first paint, before the page connects, already lists the folder tree", %{
      reader: reader,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "z"))
      html = reader |> get("/") |> html_response(200)

      paths = html |> LazyHTML.from_document() |> LazyHTML.query("#tree .node") |> LazyHTML.attribute("data-path")
      assert paths == ["sub", "z"]
    end
  end

  describe "category card" do
    test "Scenario: The folder card counts the open folder's own files by kind, by extension in any case", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "sub/a.png"), :binary.copy("a", 1000))
      File.write!(Path.join(root, "sub/B.JPG"), :binary.copy("b", 1048))
      File.write!(Path.join(root, "sub/notes.txt"), "hello")
      File.write!(Path.join(root, "sub/.x.png"), "dot")
      File.write!(Path.join(root, "sub/ignored.png"), "ignored")
      File.write!(Path.join(root, "sub/.slimserveignore"), "ignored.png\n")
      File.mkdir_p!(Path.join(root, "sub/inner"))
      File.write!(Path.join(root, "sub/inner/c.png"), :binary.copy("c", 5000))

      {:ok, view, _html} = live(reader, "/sub")

      assert card(view) == %{"image" => {"2", "2.0 KB"}, "document" => {"1", "5 B"}}
    end
  end

  describe "filter" do
    test "Scenario: The name filter survives a relist, and a hidden file that changes stays hidden", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "a1.txt"), "a")
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(reader, "/")

      filter(view, "a")
      assert names(view) == ["a1.txt"]

      File.write!(Path.join(root, "a2.txt"), "a")
      eventually(fn -> names(view) == ["a1.txt", "a2.txt"] end)

      File.write!(Path.join(root, "b.txt"), "bigger")
      File.write!(Path.join(root, "a1.txt"), "bigger")

      eventually(fn ->
        view |> doc() |> LazyHTML.query("#entries tr[data-name='a1.txt'] .size") |> text() == "6 B"
      end)

      assert names(view) == ["a1.txt", "a2.txt"]
    end
  end

  describe "inspector" do
    test "Scenario: A guest selects and filters; SVG previews as an image, HTML never embeds; no write controls", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "pic.svg"), ~s(<svg xmlns="http://www.w3.org/2000/svg"/>))
      File.write!(Path.join(root, "page.html"), "<p>hi</p>")
      {:ok, view, _html} = live(reader, "/")

      select(view, "pic.svg")
      assert has_element?(view, "#inspector[data-name='pic.svg'] img[src='/pic.svg']")

      select(view, "page.html")
      assert has_element?(view, "#inspector[data-name='page.html']")

      for selector <- ~w(iframe object embed img) do
        refute has_element?(view, "#inspector " <> selector)
      end

      refute has_element?(view, "#rename-form")
      refute has_element?(view, "#inspector-delete")
      refute has_element?(view, "#move-dialog")

      filter(view, "pic")
      assert names(view) == ["pic.svg"]
    end

    test "Scenario: Only the last selected row is marked, and a row the filter hid stays hidden", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(reader, "/")

      select(view, "a.txt")
      select(view, "b.txt")
      assert selected(view) == ["b.txt"]

      select(view, "a.txt")
      filter(view, "b.")
      select(view, "b.txt")
      assert names(view) == ["b.txt"]
      assert selected(view) == ["b.txt"]
    end

    test "Scenario: An image's kind line carries its pixel size once its thumbnail is made", %{reader: reader, root: root} do
      :ok = 200 |> Image.build_image!(100, [200, 80, 40]) |> Image.write_to_file(Path.join(root, "pic.png"))
      kind_line = fn view -> view |> doc() |> LazyHTML.query("#inspector .kindline") |> text() end

      {:ok, view, _html} = live(reader, "/")
      select(view, "pic.png")
      assert kind_line.(view) == "PNG image"

      assert reader |> get("/pic.png?thumb=1") |> response(200)
      _ = :sys.get_state(SlimServe.ThumbCache)

      {:ok, view, _html} = live(reader, "/")
      select(view, "pic.png")
      assert kind_line.(view) == "PNG image · 200 × 100"
    end

    test "Scenario: A JPEG turned a quarter by its EXIF orientation shows its size as displayed", %{
      reader: reader,
      root: root
    } do
      {:ok, turned} =
        200 |> Image.build_image!(100, [200, 80, 40]) |> Image.mutate(&MutableImage.set(&1, "orientation", :gint, 6))

      :ok = Image.write_to_file(turned, Path.join(root, "turned.jpg"))
      assert reader |> get("/turned.jpg?thumb=1") |> response(200)
      _ = :sys.get_state(SlimServe.ThumbCache)

      {:ok, view, _html} = live(reader, "/")
      select(view, "turned.jpg")
      assert view |> doc() |> LazyHTML.query("#inspector .kindline") |> text() == "JPG image · 100 × 200"
    end

    test "Scenario: The selection clears when its file is removed outside the page", %{reader: reader, root: root} do
      File.write!(Path.join(root, "x.txt"), "x")
      {:ok, view, _html} = live(reader, "/")

      select(view, "x.txt")
      assert has_element?(view, "#inspector[data-name='x.txt']")

      File.rm!(Path.join(root, "x.txt"))
      eventually(fn -> not has_element?(view, "#inspector") end)
    end

    test "Scenario: A rename keeps the entry in its folder and the inspector follows it; a name with a slash is refused",
         %{
           writer: writer,
           root: root
         } do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      File.mkdir_p!(Path.join(root, "sub/inner"))
      {:ok, view, _html} = live(writer, "/sub")

      select(view, "x.txt")
      view |> element("#rename-form") |> render_submit(%{"name" => "y.txt"})
      assert File.read(Path.join(root, "sub/y.txt")) == {:ok, "x"}
      eventually(fn -> has_element?(view, "#inspector[data-name='y.txt']") end)

      before = disk(root)
      view |> element("#rename-form") |> render_submit(%{"name" => "inner/z.txt"})
      view |> element("#rename-form") |> render_submit(%{"name" => ".hidden"})
      assert disk(root) == before

      select(view, "inner")
      view |> element("#rename-form") |> render_submit(%{"name" => "outer"})
      assert File.dir?(Path.join(root, "sub/outer"))
      refute File.exists?(Path.join(root, "sub/inner"))
    end

    test "Scenario: Move to… moves the file into the folder picked in the dialog", %{writer: writer, root: root} do
      File.write!(Path.join(root, "a.txt"), "a")
      File.mkdir_p!(Path.join(root, "sub/deep"))
      {:ok, view, _html} = live(writer, "/")

      select(view, "a.txt")
      view |> element("#move-open") |> render_click()
      view |> element("#picker .node[data-path='sub'] .chev") |> render_click()
      view |> element("#picker .node[data-path='sub/deep'] .pick") |> render_click()
      view |> element("#move-form") |> render_submit()

      assert File.read!(Path.join(root, "sub/deep/a.txt")) == "a"
      refute File.exists?(Path.join(root, "a.txt"))
    end

    test "Scenario: Move to… opens on the current folders each time, with nothing expanded from before", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.mkdir_p!(Path.join(root, "sub/deep"))
      {:ok, view, _html} = live(writer, "/")

      select(view, "a.txt")
      view |> element("#move-open") |> render_click()
      view |> element("#picker .node[data-path='sub'] .chev") |> render_click()
      assert has_element?(view, "#picker .node[data-path='sub/deep']")

      File.mkdir_p!(Path.join(root, "sub/new"))
      view |> element("#move-open") |> render_click()
      refute has_element?(view, "#picker .node[data-path='sub/deep']")

      view |> element("#picker .node[data-path='sub'] .chev") |> render_click()
      assert has_element?(view, "#picker .node[data-path='sub/new']")
    end

    test "Scenario: A rename or move onto the entry's own place changes nothing and reports nothing", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      File.mkdir_p!(Path.join(root, "z"))
      {:ok, view, _html} = live(writer, "/sub")
      before = disk(root)

      select(view, "x.txt")
      view |> element("#rename-form") |> render_submit(%{"name" => "x.txt"})
      refute has_element?(view, "#flash-error")

      view |> element("#move-open") |> render_click()
      view |> element("#picker .node[data-path='sub'] .pick") |> render_click()
      assert has_element?(view, "#move-form button[type=submit][disabled]")
      view |> element("#move-form") |> render_submit()
      refute has_element?(view, "#flash-error")

      # What the drag hook sends for a drop on the open folder's own tree node.
      render_hook(view, "rename", %{"path" => "/sub/x.txt", "to" => "sub/x.txt"})
      refute has_element?(view, "#flash-error")
      refute has_element?(view, "#tree .node[data-path='sub'][data-drop]")
      assert has_element?(view, "#tree .node[data-path='z'][data-drop='z']")

      assert disk(root) == before
    end
  end

  describe "in-page navigation" do
    # The browser probe covers the history it keeps.
    defp open(view, path) do
      render_hook(view, "open", %{"path" => path})
      view
    end

    test "Scenario: A guest opens a subfolder: the list, crumbs, tree, title and selection follow, and its changes show",
         %{
           reader: reader,
           root: root
         } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "sub/x.txt"), "x")
      File.mkdir_p!(Path.join(root, "sub/deep"))
      {:ok, view, _html} = live(reader, "/")

      select(view, "a.txt")
      filter(view, "a")
      open(view, "/sub")

      assert_reply(view, %{ok: true})
      assert names(view) == ["deep", "x.txt"]
      assert view |> doc() |> LazyHTML.query("#crumbs a") |> LazyHTML.attribute("href") == ["/", "/sub"]
      assert tree(view) == [{"sub", "2", true, true}, {"sub/deep", "0", false, false}]
      assert selected(view) == []
      refute has_element?(view, "#inspector")
      assert page_title(view) == "/sub"
      assert has_element?(view, "#listing[data-path='/sub']")

      File.write!(Path.join(root, "sub/y.txt"), "y")
      eventually(fn -> names(view) == ["deep", "x.txt", "y.txt"] end)
    end

    test "Scenario: Opening a file, a missing, hidden or ignored folder, a .. path or an encoded trick is refused, the page unchanged",
         %{
           reader: reader,
           root: root
         } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.mkdir_p!(Path.join(root, ".hidden"))
      File.mkdir_p!(Path.join(root, "ign"))
      File.write!(Path.join(root, ".slimserveignore"), "ign\n")
      File.mkdir_p!(Path.join(root, "sub/inner"))
      {:ok, view, _html} = live(reader, "/sub")
      select(view, "inner")
      before = render(view)

      for path <- ~w(/a.txt /nope /.hidden /ign /sub/.. /sub/../ign /sub/%2E%2E /sub%2F..%2Fign /%zz) do
        open(view, path)
        assert_reply(view, %{ok: false})
        assert render(view) == before, "#{path} changed the page"
      end
    end

    test "Scenario: Opening a path that is not a string is refused, and the page lives on", %{reader: reader} do
      {:ok, view, _html} = live(reader, "/")
      # So a crash of the page fails the assertions below instead of killing the test.
      Process.flag(:trap_exit, true)

      try do
        open(view, 1)
      catch
        :exit, _reason -> :crashed
      end

      assert_reply(view, %{ok: false})
      assert has_element?(view, "#listing[data-path='/']")
    end

    test "Scenario: Opening a folder that resolves but cannot be listed is refused, the page unchanged", %{
      reader: reader,
      root: root
    } do
      locked = Path.join(root, "locked")
      File.mkdir_p!(locked)
      {:ok, view, _html} = live(reader, "/")
      File.chmod!(locked, 0o311)
      on_exit(fn -> File.chmod(locked, 0o755) end)
      before = render(view)

      open(view, "/locked")

      assert_reply(view, %{ok: false})
      assert render(view) == before
    end

    test "Scenario: After opening a subfolder, a change in the folder it left no longer relists the page", %{
      reader: reader,
      root: root
    } do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      {:ok, view, _html} = live(reader, "/")
      open(view, "/sub")
      assert names(view) == ["x.txt"]

      # The root's ignore file hides x.txt from any later listing of sub, so only a relist could drop it.
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, Root.topic(""))
      File.write!(Path.join(root, ".slimserveignore"), "x.txt\n")
      assert_receive {:dir_changed, _dir}, @within
      _ = :sys.get_state(view.pid)

      # A page still on the root's topic relists 100 ms after the change message.
      refute_receive :never, 300
      _ = :sys.get_state(view.pid)
      assert names(view) == ["x.txt"]
    end
  end

  describe "row menu" do
    test "Scenario: Opening a row's menu and deleting from it leaves the selection where it was", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(writer, "/")

      select(view, "a.txt")
      view |> element("#entries tr[data-name='b.txt'] .menu button[phx-click=delete]") |> render_click()

      refute File.exists?(Path.join(root, "b.txt"))
      assert File.exists?(Path.join(root, "a.txt"))
      assert selected(view) == ["a.txt"]
      assert has_element?(view, "#inspector[data-name='a.txt']")
    end

    test "Scenario: Move to… from a row's menu opens the dialog on that row and leaves the selection and the inspector",
         %{
           writer: writer,
           root: root
         } do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(writer, "/")

      select(view, "a.txt")
      render_hook(view, "move_open", %{"path" => "/b.txt"})
      assert_push_event(view, "move_ready", %{})
      assert selected(view) == ["a.txt"]
      assert has_element?(view, "#inspector[data-name='a.txt']")

      view |> element("#picker .node[data-path='sub'] .pick") |> render_click()
      view |> element("#move-form") |> render_submit()
      assert File.exists?(Path.join(root, "sub/b.txt"))
      assert File.exists?(Path.join(root, "a.txt"))
    end

    test "Scenario: The row menu offers reads to everyone and writes only with write rights", %{
      reader: reader,
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "a")
      menu = "#entries tr[data-name='a.txt'] .menu "

      {:ok, view, _html} = live(writer, "/")
      assert has_element?(view, menu <> "a[download][href='/a.txt']")
      assert has_element?(view, menu <> "[data-act=copy]")
      assert has_element?(view, menu <> "[data-act=rename]")
      assert has_element?(view, menu <> "[data-act=move]")
      assert has_element?(view, menu <> "button[phx-click=delete][phx-value-path='/a.txt']")

      {:ok, view, _html} = live(reader, "/")
      assert has_element?(view, menu <> "a[download][href='/a.txt']")
      assert has_element?(view, menu <> "[data-act=copy]")
      refute has_element?(view, menu <> "[data-act=rename]")
      refute has_element?(view, menu <> "[data-act=move]")
      refute has_element?(view, menu <> "button[phx-click=delete]")
    end
  end
end
