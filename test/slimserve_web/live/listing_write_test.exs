defmodule SlimServeWeb.ListingWriteTest do
  # Not async: the served root and the write password are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.Root

  @moduletag :tmp_dir

  # The spec's bound: a connected listing updates within a second.
  @within 1_000

  setup %{conn: conn, tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    outside = Path.join(tmp_dir, "outside")
    File.mkdir_p!(Path.join(root, "sub"))
    File.mkdir_p!(outside)
    Application.put_env(:slimserve, :root, root)
    Application.put_env(:slimserve, :write_password, "wpass")

    on_exit(fn ->
      for key <- [:root, :write_password, :upload_max_bytes, :upload_types], do: Application.delete_env(:slimserve, key)
    end)

    start_supervised!({SlimServe.Watcher, root: root})
    ready(root)

    # A fresh /64 per test, so no test's attempts reach another's rate limit.
    conn = %{conn | remote_ip: {0xFD01, 0, 0, rem(System.unique_integer([:positive]), 65_536), 0, 0, 0, 1}}
    writer = conn |> post("/.app/login", %{"password" => "wpass"}) |> recycle()
    {:ok, root: root, outside: outside, reader: conn, writer: writer}
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

  # Every path under the root, dot-files included, with a file's bytes or a link's target.
  defp tree(root) do
    for path <- Path.wildcard(Path.join(root, "**"), match_dot: true), into: %{} do
      value =
        case File.lstat!(path).type do
          :symlink -> {:link, File.read_link!(path)}
          :directory -> :dir
          _ -> File.read!(path)
        end

      {Path.relative_to(path, root), value}
    end
  end

  defp upload(view, name, content, type \\ nil) do
    input =
      file_input(view, "#upload-form", :files, [%{name: name, content: content, type: type || MIME.from_path(name)}])

    result = render_upload(input, name)
    view |> element("#upload-form") |> render_submit()
    result
  end

  defp row_id(view, name) do
    rows = view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries tr")

    Enum.find_value(rows, fn row ->
      if row |> LazyHTML.query(".name .label") |> LazyHTML.text() == name, do: row |> LazyHTML.attribute("id") |> hd()
    end)
  end

  defp rename(view, from, to), do: render_hook(view, "rename", %{"from" => from, "to" => to})
  defp remove(view, name), do: render_hook(view, "delete", %{"name" => name})

  describe "with write rights" do
    test "a writer uploads, makes a folder, moves a file there and back and deletes it, and another page follows",
         %{writer: writer, root: root} do
      {:ok, view, _html} = live(writer, "/")
      {:ok, other, _html} = live(writer, "/")

      upload(view, "a.txt", "hello")
      assert File.read!(Path.join(root, "a.txt")) == "hello"
      assert_listed(other, ["/sub", "/a.txt"])

      view |> element("#mkdir-form") |> render_submit(%{"name" => "new"})
      assert File.dir?(Path.join(root, "new"))
      assert_listed(other, ["/new", "/sub", "/a.txt"])

      view |> element("form[data-name='a.txt']") |> render_submit(%{"to" => "sub/b.txt"})
      refute File.exists?(Path.join(root, "a.txt"))
      assert File.read!(Path.join(root, "sub/b.txt")) == "hello"
      assert_listed(other, ["/new", "/sub"])

      {:ok, in_sub, _html} = live(writer, "/sub")
      rename(in_sub, "b.txt", "a.txt")
      refute File.exists?(Path.join(root, "sub/b.txt"))
      assert File.read!(Path.join(root, "a.txt")) == "hello"
      assert_listed(other, ["/new", "/sub", "/a.txt"])

      view |> element("button[phx-click=delete][phx-value-name='a.txt']") |> render_click()
      refute File.exists?(Path.join(root, "a.txt"))
      assert_listed(other, ["/new", "/sub"])
    end

    test "a successful upload leaves no temporary file", %{writer: writer, root: root} do
      {:ok, view, _html} = live(writer, "/")

      upload(view, "a.txt", "hello")
      assert Enum.sort(File.ls!(root)) == [".ready", "a.txt", "sub"]
    end

    # A relist resets the stream; an id by position would patch the row of b.txt with a.txt.
    test "a relist keeps each entry's row id", %{writer: writer, root: root} do
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(writer, "/")
      before = row_id(view, "b.txt")

      File.write!(Path.join(root, "a.txt"), "a")
      assert_listed(view, ["/sub", "/a.txt", "/b.txt"])
      assert row_id(view, "b.txt") == before
    end

    # A relative link's target would change meaning in another folder.
    test "a link is renamed within its own folder, and not moved to another", %{writer: writer, root: root} do
      File.ln_s!("sub", Path.join(root, "to-dir"))
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "to-dir", "sub/to-dir")
      assert tree(root) == before

      rename(view, "to-dir", "renamed")
      assert File.read_link!(Path.join(root, "renamed")) == "sub"
    end

    test "a folder renamed onto an existing empty folder is refused, and both stay", %{writer: writer, root: root} do
      File.mkdir_p!(Path.join(root, "x"))
      File.write!(Path.join(root, "x/f.txt"), "f")
      File.mkdir_p!(Path.join(root, "y"))
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "x", "y")
      assert tree(root) == before
    end

    test "a folder moves with its files, and a dot-file inside it does not block the move", %{writer: writer, root: root} do
      File.mkdir_p!(Path.join(root, "c/d"))
      File.write!(Path.join(root, "c/d/x.txt"), "x")
      File.write!(Path.join(root, "c/.env"), "secret")
      {:ok, view, _html} = live(writer, "/")

      rename(view, "c", "sub/c")
      refute File.exists?(Path.join(root, "c"))
      assert File.read!(Path.join(root, "sub/c/d/x.txt")) == "x"
      assert File.read!(Path.join(root, "sub/c/.env")) == "secret"
    end

    test "an upload onto an existing name is refused, leaving its bytes and no temporary file", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "a.txt"), "old")
      {:ok, view, _html} = live(writer, "/")

      upload(view, "a.txt", "new")
      assert File.read!(Path.join(root, "a.txt")) == "old"
      assert Enum.sort(File.ls!(root)) == [".ready", "a.txt", "sub"]
    end

    test "a rename or a new folder onto an existing name is refused, and both stay", %{writer: writer, root: root} do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "b.txt"), "b")
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "a.txt", "b.txt")
      rename(view, "a.txt", "sub")
      view |> element("#mkdir-form") |> render_submit(%{"name" => "a.txt"})
      assert tree(root) == before
    end

    test "a rename out of the root, to a dot-name or of a folder into its own child is refused", %{
      writer: writer,
      root: root
    } do
      File.mkdir_p!(Path.join(root, "a/b"))
      File.write!(Path.join(root, "f.txt"), "f")
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "f.txt", "../out.txt")
      rename(view, "f.txt", ".hidden")
      rename(view, "f.txt", "sub/.hidden")
      rename(view, "f.txt", "")
      rename(view, "a", "a/b/a")
      assert tree(root) == before
      refute File.exists?(Path.join(root, "../out.txt"))
    end

    test "a write aimed at a hidden or ignored name, or into an ignored folder, is refused", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, ".slimserveignore"), "build/\nsecret.txt\n/skipped\n")
      File.write!(Path.join(root, ".env"), "e")
      File.mkdir_p!(Path.join(root, "a"))
      File.mkdir_p!(Path.join(root, "skipped"))
      File.write!(Path.join(root, "f.txt"), "f")
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      view |> element("#mkdir-form") |> render_submit(%{"name" => "build"})
      rename(view, "a", "build")
      rename(view, "f.txt", "secret.txt")
      rename(view, "f.txt", "skipped/f.txt")
      rename(view, ".env", "env")
      remove(view, ".env")
      remove(view, "skipped")
      upload(view, "secret.txt", "s")
      assert tree(root) == before
    end

    # Patterns are relative to their ignore file, so the move would reveal the file at its new path.
    test "a folder holding a file an ignore pattern hides at its current path does not move", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, ".slimserveignore"), "/a/deep/secret.txt\n")
      File.mkdir_p!(Path.join(root, "a/deep"))
      File.write!(Path.join(root, "a/deep/secret.txt"), "s")
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "a", "b")
      assert tree(root) == before
    end

    test "deleting a link to a file or a folder removes the link and leaves its target", %{writer: writer, root: root} do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      File.ln_s!("sub", Path.join(root, "to-dir"))
      File.ln_s!("sub/x.txt", Path.join(root, "to-file"))
      {:ok, view, _html} = live(writer, "/")

      remove(view, "to-dir")
      remove(view, "to-file")
      assert {:error, :enoent} = File.lstat(Path.join(root, "to-dir"))
      assert {:error, :enoent} = File.lstat(Path.join(root, "to-file"))
      assert File.read!(Path.join(root, "sub/x.txt")) == "x"
    end

    test "deleting an empty folder removes it at once", %{writer: writer, root: root} do
      {:ok, view, _html} = live(writer, "/")

      remove(view, "sub")
      refute File.exists?(Path.join(root, "sub"))
    end

    test "a folder with files is deleted only after the confirm bar that counts them", %{writer: writer, root: root} do
      File.mkdir_p!(Path.join(root, "sub/deep"))
      File.write!(Path.join(root, "sub/x.txt"), "x")
      File.write!(Path.join(root, "sub/deep/.y"), "y")
      {:ok, view, _html} = live(writer, "/")

      view |> element("button[phx-click=delete][phx-value-name='sub']") |> render_click()
      assert File.dir?(Path.join(root, "sub"))
      assert has_element?(view, "#confirm-delete[data-count='2']")

      view |> element("#confirm-delete-cancel") |> render_click()
      refute has_element?(view, "#confirm-delete")
      assert File.dir?(Path.join(root, "sub"))

      view |> element("button[phx-click=delete][phx-value-name='sub']") |> render_click()
      view |> element("#confirm-delete-yes") |> render_click()
      refute File.exists?(Path.join(root, "sub"))
    end

    test "a folder that gained a file after the confirm bar opened stays, and the bar counts again", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      {:ok, view, _html} = live(writer, "/")

      remove(view, "sub")
      assert has_element?(view, "#confirm-delete[data-count='1']")

      File.write!(Path.join(root, "sub/y.txt"), "y")
      view |> element("#confirm-delete-yes") |> render_click()
      assert File.read(Path.join(root, "sub/y.txt")) == {:ok, "y"}
      assert has_element?(view, "#confirm-delete[data-count='2']")
    end

    test "a folder replaced by another of the same count after the confirm bar opened stays", %{
      writer: writer,
      root: root
    } do
      File.write!(Path.join(root, "sub/x.txt"), "x")
      {:ok, view, _html} = live(writer, "/")
      remove(view, "sub")

      File.rename!(Path.join(root, "sub"), Path.join(root, "old"))
      File.mkdir_p!(Path.join(root, "sub"))
      File.write!(Path.join(root, "sub/new.txt"), "n")
      view |> element("#confirm-delete-yes") |> render_click()
      assert File.read(Path.join(root, "sub/new.txt")) == {:ok, "n"}
    end

    test "deleting a folder that links out of the root leaves the outside folder whole", %{
      writer: writer,
      root: root,
      outside: outside
    } do
      File.write!(Path.join(outside, "keep.txt"), "k")
      File.ln_s!(outside, Path.join(root, "sub/out"))
      File.write!(Path.join(root, "sub/x.txt"), "x")
      {:ok, view, _html} = live(writer, "/")

      remove(view, "sub")
      render_hook(view, "confirm_delete", %{"name" => "sub"})
      refute File.exists?(Path.join(root, "sub"))
      assert File.read!(Path.join(outside, "keep.txt")) == "k"
    end

    test "an upload over the size limit or of a type off the list is refused, with the entry's error shown", %{
      writer: writer,
      root: root
    } do
      Application.put_env(:slimserve, :upload_max_bytes, 4)
      Application.put_env(:slimserve, :upload_types, [".txt"])
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      assert {:error, _} = upload(view, "big.txt", "too many bytes")
      assert has_element?(view, "#upload-entries .field-error")
      assert tree(root) == before

      {:ok, view, _html} = live(writer, "/")
      assert {:error, _} = upload(view, "a.png", "png")
      assert has_element?(view, "#upload-entries .field-error")
      assert tree(root) == before
    end

    # The browser names the type, so only the name's own extension is trusted.
    test "an upload named off the type list is refused, whatever type the browser claims", %{
      writer: writer,
      root: root
    } do
      Application.put_env(:slimserve, :upload_types, [".jpg"])
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      upload(view, "evil.html", "<script></script>", "image/jpeg")
      refute File.exists?(Path.join(root, "evil.html"))
      assert tree(root) == before
    end

    test "a file renamed to an extension off the type list is refused", %{writer: writer, root: root} do
      Application.put_env(:slimserve, :upload_types, [".jpg"])
      File.write!(Path.join(root, "page.jpg"), "p")
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      rename(view, "page.jpg", "page.html")
      assert tree(root) == before

      rename(view, "page.jpg", "photo.JPG")
      assert File.read!(Path.join(root, "photo.JPG")) == "p"
    end
  end

  describe "without write rights" do
    # Every event the page handles. The gate halts all of them without write rights, so this list
    # only has to reach the gate, not to be complete.
    @events ~w(validate cancel_upload upload mkdir rename delete confirm_delete cancel_delete)

    # Each event on a page of its own, so an event that crashes its page cannot keep the next one
    # from reaching the gate.
    defp each_alone(open, names) do
      Process.flag(:trap_exit, true)

      for name <- names, event <- @events do
        view = open.()

        try do
          render_hook(view, event, %{"name" => name, "from" => name, "to" => "moved-" <> name, "ref" => "0"})
        catch
          :exit, _reason -> :crashed
        end
      end
    end

    test "every event leaves the disk unchanged, and the write UI is absent", %{reader: reader, root: root} do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "sub/x.txt"), "x")
      {:ok, view, _html} = live(reader, "/")
      before = tree(root)

      refute has_element?(view, "#write-bar")
      refute has_element?(view, "button[phx-click=delete]")

      each_alone(fn -> reader |> live("/") |> elem(1) end, ["a.txt", "sub"])
      assert tree(root) == before
    end

    test "an open write page stops writing once the write password changes", %{writer: writer, root: root} do
      File.write!(Path.join(root, "a.txt"), "a")
      File.write!(Path.join(root, "sub/x.txt"), "x")
      before = tree(root)

      open = fn ->
        Application.put_env(:slimserve, :write_password, "wpass")
        {:ok, view, _html} = live(writer, "/")
        Application.put_env(:slimserve, :write_password, "changed")
        view
      end

      each_alone(open, ["a.txt", "sub"])
      assert tree(root) == before
    end

    test "a write session stops writing once the write password is unset", %{writer: writer, root: root} do
      File.write!(Path.join(root, "a.txt"), "a")
      Application.delete_env(:slimserve, :write_password)
      {:ok, view, _html} = live(writer, "/")
      before = tree(root)

      refute has_element?(view, "#write-bar")

      each_alone(fn -> writer |> live("/") |> elem(1) end, ["a.txt", "sub"])
      assert tree(root) == before
    end
  end
end
