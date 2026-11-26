defmodule SlimServe.S3IntegrationTest do
  # Not async: the storage backend and the poll period are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage.S3.Capabilities
  alias SlimServe.Storage.S3.Metadata
  alias SlimServe.ThumbCache
  alias Vix.Vips.Image

  @moduletag :s3

  setup do
    Application.put_env(:slimserve, :s3_poll_ms, 50)
    on_exit(fn -> Application.delete_env(:slimserve, :s3_poll_ms) end)
  end

  defp doc(view), do: view |> render() |> LazyHTML.from_fragment()

  defp text(node), do: node |> LazyHTML.text() |> String.trim()

  defp flush_requests(acc \\ []) do
    receive do
      {:s3_request, _method, _key, _query} = request -> flush_requests([request | acc])
    after
      0 -> Enum.reverse(acc)
    end
  end

  defp list_request?({:s3_request, :get, "", query}, prefix), do: query[:prefix] == prefix
  defp list_request?(_request, _prefix), do: false

  defp poll?({:s3_request, :get, "", query} = request, prefix),
    do: list_request?(request, prefix) and query[:delimiter] == "/"

  defp poll?(_request, _prefix), do: false

  defp await_lists(prefix, n, seen \\ []) do
    if Enum.count(seen, &poll?(&1, prefix)) >= n do
      seen
    else
      receive do
        {:s3_request, _method, _key, _query} = request -> await_lists(prefix, n, seen ++ [request])
      after
        1_000 -> flunk("the page never polled")
      end
    end
  end

  defp eventually(check, deadline \\ System.monotonic_time(:millisecond) + 1_000) do
    cond do
      check.() -> :ok
      System.monotonic_time(:millisecond) > deadline -> flunk("the page never reached the expected state")
      true -> receive(after: (20 -> eventually(check, deadline)))
    end
  end

  test "the tree and the card show a bucket's folders and files, and an unchanged poll lists no subfolder", %{
    conn: conn
  } do
    bucket = S3.serve()
    S3.put(bucket, "a/x.png", String.duplicate("x", 1000))
    S3.put(bucket, "a/B.JPG", String.duplicate("b", 1048))
    S3.put(bucket, "a/notes.txt", "hello")
    S3.put(bucket, "a/.hidden", "h")
    S3.put(bucket, "a/sub/c.png", "c")
    S3.put(bucket, "a/sub/d.txt", "d")
    S3.put(bucket, "z/y.txt", "y")
    S3.put(bucket, "r.txt", "r")

    {:ok, view, _html} = live(conn, "/a")

    tree =
      for node <- view |> doc() |> LazyHTML.query("#tree .node") do
        {node |> LazyHTML.attribute("data-path") |> hd(), node |> LazyHTML.query(".ct") |> text()}
      end

    assert tree == [{"a", "4"}, {"a/sub", "2"}, {"z", "1"}]

    card =
      for cat <- view |> doc() |> LazyHTML.query("#card .cat"), into: %{} do
        {cat |> LazyHTML.attribute("data-kind") |> hd(),
         {cat |> LazyHTML.query(".n") |> text(), cat |> LazyHTML.query(".size") |> text()}}
      end

    assert card == %{"image" => {"2", "2.0 KB"}, "document" => {"1", "5 B"}}

    # An empty metadata cache, so a tree rebuilt now would have to list each folder it shows.
    :ok = Metadata.changed([], [])
    S3.watch_requests()

    requests = await_lists("a/", 2)
    _ = :sys.get_state(view.pid)
    requests = requests ++ flush_requests()

    refute Enum.any?(requests, &(list_request?(&1, "a/sub/") or list_request?(&1, "z/")))
  end

  test "an unfolded level drops a subfolder another page deletes", %{conn: conn} do
    bucket = S3.serve()
    S3.put(bucket, "z/y/f.txt", "f")
    S3.put(bucket, "z/w/g.txt", "g")

    nodes = fn view ->
      for node <- view |> doc() |> LazyHTML.query("#tree .node"), do: hd(LazyHTML.attribute(node, "data-path"))
    end

    {:ok, view, _html} = live(conn, "/")
    view |> element("#tree .node[data-path='z'] button.chev") |> render_click()
    assert nodes.(view) == ["z", "z/w", "z/y"]

    # So the metadata cache sees the change as it does for any page's delete.
    {:confirm, asked} = SlimServe.Storage.delete(["z"], "y")
    :ok = SlimServe.Storage.confirm_delete(["z"], "y", asked)

    eventually(fn -> nodes.(view) == ["z", "z/w"] end)
  end

  defp names(view), do: view |> doc() |> LazyHTML.query("#entries .name .label") |> Enum.map(&text/1)

  @tag :tmp_dir
  test "a write in the open folder counts none of its unchanged subfolders again", %{conn: conn, tmp_dir: tmp_dir} do
    # So the only re-list is the one the write's change message asks for.
    Application.put_env(:slimserve, :s3_poll_ms, 60_000)
    bucket = S3.serve()
    S3.put(bucket, "a/sub/c.txt", "c")
    S3.put(bucket, "a/w/d.txt", "d")
    S3.put(bucket, "z/y.txt", "y")
    Capabilities.check()
    {:ok, view, _html} = live(conn, "/a")
    S3.watch_requests()

    file = Path.join(tmp_dir, "n.txt")
    File.write!(file, "n")
    :ok = SlimServe.Storage.upload(["a"], "n.txt", file)
    eventually(fn -> "n.txt" in names(view) end)
    _ = :sys.get_state(view.pid)

    refute Enum.any?(
             flush_requests(),
             &(list_request?(&1, "a/sub/") or list_request?(&1, "a/w/") or list_request?(&1, "z/"))
           )
  end

  test "a change made while the page lists its folder still reaches the page", %{conn: conn} do
    Application.put_env(:slimserve, :s3_poll_ms, 60_000)
    bucket = S3.serve()
    S3.put(bucket, "a/x.txt", "x")
    conn = get(conn, "/a")
    # An empty cache, so the connected mount asks the store for the listing.
    :ok = Metadata.changed([], [])

    # Right after the connected mount's own listing of the folder returns, before the page uses it.
    id = make_ref()

    :ok =
      :telemetry.attach(
        id,
        [:slimserve, :s3, :request],
        fn _event, _measurements, meta, _config ->
          if poll?({:s3_request, meta.method, meta.key, meta.query}, "a/") do
            :telemetry.detach(id)
            S3.put(bucket, "a/late.txt", "l")
            :ok = Metadata.changed([["a", "late.txt"]])
          end
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(id) end)

    {:ok, view, _html} = live(conn)
    eventually(fn -> "late.txt" in names(view) end)
  end

  @tag :tmp_dir
  test "an image's kind line carries its pixel size once its thumbnail is made", %{conn: conn, tmp_dir: tmp_dir} do
    # An empty cache, so the size can only come from this test's thumbnail.
    :ok = Supervisor.terminate_child(SlimServe.Supervisor, ThumbCache)
    on_exit(fn -> {:ok, _pid} = Supervisor.restart_child(SlimServe.Supervisor, ThumbCache) end)
    start_supervised!({ThumbCache, dir: tmp_dir, max_bytes: 1024 * 1024})

    bucket = S3.serve()
    {:ok, png} = 200 |> Image.build_image!(100, [200, 80, 40]) |> Image.write_to_buffer(".png")
    S3.put(bucket, "pic.png", png)
    kind_line = fn view -> view |> doc() |> LazyHTML.query("#inspector .kindline") |> text() end

    {:ok, view, _html} = live(conn, "/")
    view |> element("#entries tr[data-name='pic.png']") |> render_click()
    assert kind_line.(view) == "PNG image"

    assert conn |> get("/pic.png?thumb=1") |> response(200)
    _ = :sys.get_state(ThumbCache)

    {:ok, view, _html} = live(conn, "/")
    view |> element("#entries tr[data-name='pic.png']") |> render_click()
    assert kind_line.(view) == "PNG image · 200 × 100"
  end
end
