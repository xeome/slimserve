defmodule SlimServe.S3ClusterTest do
  # Not async: this node joins a cluster, and the storage backend is application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest
  import SlimServeWeb.ListingHelpers

  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage
  alias SlimServe.Storage.S3.Capabilities

  @moduletag :s3
  @moduletag :tmp_dir
  @moduletag capture_log: true

  setup %{tmp_dir: tmp_dir} do
    if not Node.alive?(), do: {:ok, _pid} = Node.start(:"slimserve-a@127.0.0.1", name_domain: :longnames)
    bucket = S3.serve()

    args = [~c"-setcookie", Atom.to_charlist(Node.get_cookie()) | Enum.flat_map(:code.get_path(), &[~c"-pa", &1])]
    {:ok, _peer, b} = :peer.start_link(%{name: :peer.random_name(), host: ~c"127.0.0.1", longnames: true, args: args})

    for {app, _description, _version} <- Application.loaded_applications() do
      :erpc.call(b, Application, :load, [app])
      :ok = :erpc.call(b, Application, :put_all_env, [[{app, Application.get_all_env(app)}]])
    end

    cache = [download_cache_dir: tmp_dir, download_cache_bytes: 10_000_000]
    :ok = :erpc.call(b, Application, :put_all_env, [[slimserve: cache]])
    {:ok, _apps} = :erpc.call(b, Application, :ensure_all_started, [:slimserve])
    {:ok, _apps} = :erpc.call(b, Application, :ensure_all_started, [:ex_unit])
    await_pubsub(b)

    {:ok, bucket: bucket, b: b}
  end

  defp await_pubsub(b) do
    {ref, groups} = :pg.monitor_scope(Phoenix.PubSub)
    if not Enum.any?(List.flatten(Map.values(groups)), &(node(&1) == b)), do: await_join(ref, b)
    :pg.demonitor(Phoenix.PubSub, ref)
  end

  defp await_join(ref, b) do
    assert_receive {^ref, :join, _group, pids}
    if not Enum.any?(pids, &(node(&1) == b)), do: await_join(ref, b)
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
