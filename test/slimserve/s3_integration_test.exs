defmodule SlimServe.S3IntegrationTest do
  # Not async: the storage backend is application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest
  import SlimServeWeb.ListingHelpers

  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage
  alias SlimServe.Storage.S3.Capabilities
  alias SlimServe.Storage.S3.Metadata
  alias SlimServe.ThumbCache
  alias Vix.Vips.Image

  @moduletag :s3

  defp doc(view), do: view |> render() |> LazyHTML.from_fragment()

  defp text(node), do: node |> LazyHTML.text() |> String.trim()

  defp flush_requests(acc \\ []) do
    receive do
      {:s3_request, _method, _key, _query} = request -> flush_requests([request | acc])
      {:s3_call, _callers, _method, _key, _query} = call -> flush_requests([call | acc])
    after
      0 -> Enum.reverse(acc)
    end
  end

  defp list_request?({:s3_request, :get, "", query}, prefix), do: query[:prefix] == prefix
  defp list_request?({:s3_call, _callers, :get, "", query}, prefix), do: query[:prefix] == prefix
  defp list_request?(_request, _prefix), do: false

  defp relist?({:s3_request, :get, "", query} = request, prefix),
    do: list_request?(request, prefix) and query[:delimiter] == "/"

  defp relist?(_request, _prefix), do: false

  # The table belongs to the process, so a new one starts empty.
  defp empty_cache do
    stop_supervised!(Metadata)
    start_supervised!(Metadata)
  end

  defp await_list(pid, prefix, seen \\ []) do
    assert_receive {:s3_call, callers, method, key, query} = call
    seen = [call | seen]

    if pid in callers and relist?({:s3_request, method, key, query}, prefix),
      do: Enum.reverse(seen),
      else: await_list(pid, prefix, seen)
  end

  defp upload(tmp_dir, segments, name) do
    file = Path.join(tmp_dir, name)
    File.write!(file, name)
    Storage.upload(segments, name, file)
  end

  @tag :tmp_dir
  test "Scenario: The tree and the card show a bucket's folders and files, and a change lists no unchanged subfolder again",
       %{
         conn: conn,
         tmp_dir: tmp_dir
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
    Capabilities.check()

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
    empty_cache()
    S3.watch_callers()
    :ok = upload(tmp_dir, ["a"], "new.png")
    await_page(view, &("new.png" in names(&1)))

    requests = await_list(view.pid, "a/") ++ flush_requests()

    refute Enum.any?(requests, &(list_request?(&1, "a/sub/") or list_request?(&1, "z/")))
  end

  test "Scenario: On a bucket, an unfolded level drops a subfolder another page deletes", %{conn: conn} do
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
    {:confirm, asked} = Storage.delete(["z"], "y")
    :ok = Storage.confirm_delete(["z"], "y", asked)

    await_page(view, &(nodes.(&1) == ["z", "z/w"]))
  end

  defp names(view), do: view |> doc() |> LazyHTML.query("#entries .name .label") |> Enum.map(&text/1)

  @tag :tmp_dir
  test "Scenario: A write in the open folder counts none of its unchanged subfolders again", %{
    conn: conn,
    tmp_dir: tmp_dir
  } do
    bucket = S3.serve()
    S3.put(bucket, "a/sub/c.txt", "c")
    S3.put(bucket, "a/w/d.txt", "d")
    S3.put(bucket, "z/y.txt", "y")
    Capabilities.check()
    {:ok, view, _html} = live(conn, "/a")
    # So a re-count of an unchanged subfolder would have to reach the store.
    empty_cache()
    S3.watch_requests()

    :ok = upload(tmp_dir, ["a"], "n.txt")
    await_page(view, &("n.txt" in names(&1)))
    _ = :sys.get_state(view.pid)

    refute Enum.any?(
             flush_requests(),
             &(list_request?(&1, "a/sub/") or list_request?(&1, "a/w/") or list_request?(&1, "z/"))
           )
  end

  @tag :tmp_dir
  test "Scenario: A change made while the page lists its folder still reaches the page", %{conn: conn, tmp_dir: tmp_dir} do
    bucket = S3.serve()
    S3.put(bucket, "a/x.txt", "x")
    Capabilities.check()
    conn = get(conn, "/a")
    # An empty cache, so the connected mount asks the store for the listing.
    empty_cache()

    # Right after the connected mount's own listing of the folder returns, before the page uses it.
    id = make_ref()
    test = self()

    :ok =
      :telemetry.attach(
        id,
        [:slimserve, :s3, :request],
        fn _event, _measurements, meta, _config ->
          if relist?({:s3_request, meta.method, meta.key, meta.query}, "a/") do
            :telemetry.detach(id)
            send(test, {:uploaded, upload(tmp_dir, ["a"], "late.txt")})
          end
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(id) end)

    {:ok, view, _html} = live(conn)
    assert_receive {:uploaded, :ok}
    await_page(view, &("late.txt" in names(&1)))
  end

  @tag :tmp_dir
  test "Scenario: On a bucket, an image's kind line carries its pixel size once its thumbnail is made", %{
    conn: conn,
    tmp_dir: tmp_dir
  } do
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
