defmodule SlimServe.S3ClusterTest do
  # Not async: this node joins a cluster, and the storage backend is application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest
  import SlimServeWeb.ListingHelpers

  alias SlimServe.Cluster
  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage
  alias SlimServe.Storage.S3.Capabilities

  @moduletag :s3
  @moduletag :tmp_dir
  @moduletag capture_log: true

  setup %{tmp_dir: tmp_dir} do
    bucket = S3.serve()
    b = Cluster.start_peer(download_cache_dir: tmp_dir, download_cache_bytes: 10_000_000)
    {:ok, bucket: bucket, b: b}
  end

  test "Scenario: A write on one node shows at once on another node's open page and next request", %{bucket: bucket, b: b} do
    S3.put(bucket, "a.txt", "a")

    # B has read both the folder and the name about to be made, so its cache holds the old answers.
    assert {:ok, [%{name: "a.txt"}]} = :erpc.call(b, Storage, :list, [[]])
    assert :erpc.call(b, Storage, :resolve, [["docs"]]) == :error
    {:ok, view, _html} = :erpc.call(b, S3, :hold_view, ["/", self()])
    assert hrefs(view) == ["/a.txt"]

    Capabilities.check()
    assert Storage.mkdir([], "docs") == :ok

    await_page(view, &(hrefs(&1) == ["/docs", "/a.txt"]))
    assert {:dir, _prefix} = :erpc.call(b, Storage, :resolve, [["docs"]])
  end

  defp hrefs(view),
    do: view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries .name a") |> LazyHTML.attribute("href")
end
