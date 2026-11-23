defmodule SlimServeWeb.ThumbnailTest do
  # Not async: the served root is application env and the cache is a named process.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.ThumbCache
  alias Vix.Vips.Image
  alias Vix.Vips.MutableImage

  @moduletag :tmp_dir

  # config/test.exs sets thumb_max_dim to this, and thumb_max_file_bytes to 1 MB.
  @max 64

  setup %{tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    File.mkdir_p!(root)
    Application.put_env(:slimserve, :root, root)
    on_exit(fn -> Application.delete_env(:slimserve, :root) end)

    # An empty cache per test, so a first request can only miss.
    :ok = Supervisor.terminate_child(SlimServe.Supervisor, ThumbCache)
    on_exit(fn -> {:ok, _pid} = Supervisor.restart_child(SlimServe.Supervisor, ThumbCache) end)
    start_supervised!({ThumbCache, dir: Path.join(tmp_dir, "cache"), max_bytes: 1024 * 1024})

    {:ok, root: root}
  end

  defp image(root, name, width, height, colour \\ [200, 80, 40]) do
    path = Path.join(root, name)
    :ok = width |> Image.build_image!(height, colour) |> Image.write_to_file(path)
    path
  end

  # The cache stores with a cast, so this returns once the thumbnail is on disk.
  defp stored, do: :sys.get_state(ThumbCache)

  defp fields(image) do
    {:ok, image} = Image.new_from_buffer(image)
    {:ok, fields} = Image.header_field_names(image)
    fields
  end

  defp thumb(conn, name), do: get(conn, "/#{name}?thumb=1")

  defp size(webp) do
    {:ok, image} = Image.new_from_buffer(webp)
    {Image.width(image), Image.height(image)}
  end

  describe "On-demand thumbnail" do
    test "Scenario: Each image format answers a WebP that fits the configured size", %{conn: conn, root: root} do
      image(root, "tall.JPG", 600, 1000)
      for name <- ~w(a.jpeg a.png a.gif a.webp), do: image(root, name, 1000, 600)

      for name <- ~w(tall.JPG a.jpeg a.png a.gif a.webp) do
        conn = thumb(conn, name)

        assert <<"RIFF", _::32, "WEBP", _::binary>> = body = response(conn, 200)
        assert get_resp_header(conn, "content-type") == ["image/webp"]
        {width, height} = size(body)
        assert max(width, height) == @max, name
      end
    end

    test "Scenario: An image smaller than the configured size is not enlarged", %{conn: conn, root: root} do
      image(root, "small.png", 20, 10)

      assert <<"RIFF", _::32, "WEBP", _::binary>> = body = conn |> thumb("small.png") |> response(200)
      assert size(body) == {20, 10}
    end

    test "Scenario: A path that is not an image answers 404", %{conn: conn, root: root} do
      File.write!(Path.join(root, "a.txt"), "alpha")
      File.write!(Path.join(root, "fake.png"), "not an image")
      File.mkdir_p!(Path.join(root, "dir.png"))

      for name <- ~w(a.txt fake.png dir.png missing.png) do
        assert thumb(conn, name).status == 404, name
      end
    end

    test "Scenario: A file that holds another format than its name says answers 404", %{conn: conn, root: root} do
      # An SVG renderer follows the href, so the thumbnail would show the hidden file.
      image(root, ".secret.png", 100, 60)

      File.write!(
        Path.join(root, "evil.png"),
        ~s(<svg xmlns="http://www.w3.org/2000/svg" width="100" height="60"><image href=".secret.png" width="100" height="60"/></svg>)
      )

      assert thumb(conn, "evil.png").status == 404
    end

    test "Scenario: A thumbnail carries none of the source's metadata", %{conn: conn, root: root} do
      path = Path.join(root, "exif.jpg")
      plain = Image.build_image!(100, 60, [200, 80, 40])
      {:ok, tagged} = Image.mutate(plain, &MutableImage.set(&1, "exif-ifd0-Artist", :gchararray, "someone"))
      :ok = Image.write_to_file(tagged, path)
      assert "exif-data" in fields(File.read!(path))

      refute "exif-data" in (conn |> thumb("exif.jpg") |> response(200) |> fields())
    end

    test "Scenario: An ignored image answers 404 while its twin answers 200", %{conn: conn, root: root} do
      image(root, "secret.png", 100, 60)
      image(root, "shown.png", 100, 60)
      File.write!(Path.join(root, ".slimserveignore"), "secret.png\n")

      assert thumb(conn, "secret.png").status == 404
      assert conn |> thumb("shown.png") |> get_resp_header("content-type") == ["image/webp"]
    end

    test "Scenario: An image over the input size limit answers 413", %{conn: conn, root: root} do
      # Random bytes: code that decodes before it checks the size answers 404.
      File.write!(Path.join(root, "big.png"), :crypto.strong_rand_bytes(1_100_000))

      assert thumb(conn, "big.png").status == 413
    end

    test "Scenario: The listing shows a thumbnail for each image it can make one for", %{conn: conn, root: root} do
      image(root, "a.png", 100, 60)
      File.write!(Path.join(root, "a.txt"), "alpha")
      File.write!(Path.join(root, "big.png"), :crypto.strong_rand_bytes(1_100_000))

      {:ok, view, _html} = live(conn, "/")
      srcs = view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries img") |> LazyHTML.attribute("src")

      assert srcs == ["/a.png?thumb=1"]
      assert conn |> get(hd(srcs)) |> get_resp_header("content-type") == ["image/webp"]
    end
  end

  describe "Thumbnail cache" do
    test "Scenario: A repeated request is served from the cache", %{conn: conn, root: root} do
      image(root, "a.png", 1000, 600)

      first = thumb(conn, "a.png")
      stored()
      again = thumb(conn, "a.png")

      assert get_resp_header(first, "x-cache") == ["miss"]
      assert get_resp_header(again, "x-cache") == ["hit"]
      assert response(again, 200) == response(first, 200)
    end

    test "Scenario: A changed source file misses the cache", %{conn: conn, root: root} do
      image(root, "a.png", 1000, 600)
      first = thumb(conn, "a.png")
      stored()

      # File times have one-second resolution, so the rewrite is dated apart from the first write.
      root |> image("a.png", 1000, 700) |> File.touch!(System.os_time(:second) + 5)
      changed = thumb(conn, "a.png")

      assert get_resp_header(changed, "x-cache") == ["miss"]
      assert size(response(changed, 200)) != size(response(first, 200))
    end

    test "Scenario: A replacement with the same size and time misses the cache", %{conn: conn, root: root} do
      path = image(root, "a.png", 100, 60)
      old = File.stat!(path, time: :posix)
      first = thumb(conn, "a.png")
      stored()

      # Another file moved onto the name, so the inode differs whatever the clock reads.
      new = image(Path.dirname(root), "new.png", 100, 60, [40, 80, 200])
      File.touch!(new, old.mtime)
      File.rename!(new, path)
      assert %{size: size, mtime: mtime} = File.stat!(path, time: :posix)
      assert {size, mtime} == {old.size, old.mtime}

      changed = thumb(conn, "a.png")

      assert get_resp_header(changed, "x-cache") == ["miss"]
      assert response(changed, 200) != response(first, 200)
    end

    test "Scenario: A thumbnail is served while the cache process is down", %{conn: conn, root: root} do
      image(root, "a.png", 100, 60)
      image(root, "b.png", 100, 60)
      thumb(conn, "a.png")
      stored()
      stop_supervised!(ThumbCache)

      assert conn |> thumb("a.png") |> get_resp_header("x-cache") == ["hit"]
      assert conn |> thumb("b.png") |> get_resp_header("x-cache") == ["miss"]
    end
  end
end
