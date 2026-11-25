defmodule SlimServe.S3StorageTest do
  # Not async: the storage backend, the poll period and the process environment are global.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage
  alias SlimServe.Storage.S3.Capabilities
  alias SlimServe.Storage.S3.Metadata

  describe "runtime configuration" do
    @variables ~w(SLIMSERVE_STORAGE SLIMSERVE_S3_BUCKET AWS_ENDPOINT_URL_S3 AWS_REGION AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN SLIMSERVE_DOWNLOAD_CACHE_DIR SLIMSERVE_ROOT PHX_SERVER SLIMSERVE_USERNAME SLIMSERVE_PASSWORD SLIMSERVE_WRITE_PASSWORD SLIMSERVE_HTTPS)

    setup do
      saved = Map.new(@variables, &{&1, System.get_env(&1)})
      on_exit(fn -> Enum.each(saved, fn {name, value} -> set(name, value) end) end)
    end

    defp set(name, nil), do: System.delete_env(name)
    defp set(name, value), do: System.put_env(name, value)

    defp prod(vars) do
      for name <- @variables, do: set(name, vars[name])
      "config/runtime.exs" |> Config.Reader.read!(env: :prod) |> Keyword.fetch!(:slimserve)
    end

    @s3 %{
      "SLIMSERVE_STORAGE" => "s3",
      "SLIMSERVE_S3_BUCKET" => "files",
      "AWS_ENDPOINT_URL_S3" => "http://minio:9000",
      "AWS_ACCESS_KEY_ID" => "id",
      "AWS_SECRET_ACCESS_KEY" => "secret",
      "PHX_SERVER" => "true"
    }

    test "an S3 server needs no root, and takes its bucket and the standard AWS variables" do
      config = prod(@s3)
      assert {config[:storage], config[:root]} == {:s3, nil}

      assert Keyword.take(config[:s3], [:bucket, :endpoint, :region, :access_key_id, :secret_access_key]) ==
               [
                 bucket: "files",
                 endpoint: "http://minio:9000",
                 region: "us-east-1",
                 access_key_id: "id",
                 secret_access_key: "secret"
               ]
    end

    test "an empty download cache directory takes the default" do
      assert prod(Map.put(@s3, "SLIMSERVE_DOWNLOAD_CACHE_DIR", ""))[:download_cache_dir] ==
               Path.join(System.tmp_dir!(), "slimserve-downloads")
    end

    test "an S3 server without a bucket or credentials, or an unknown backend, refuses to boot" do
      assert_raise RuntimeError, ~r/SLIMSERVE_S3_BUCKET/, fn -> prod(Map.delete(@s3, "SLIMSERVE_S3_BUCKET")) end
      assert_raise RuntimeError, ~r/AWS_SECRET_ACCESS_KEY/, fn -> prod(Map.delete(@s3, "AWS_SECRET_ACCESS_KEY")) end
      assert_raise RuntimeError, ~r/SLIMSERVE_STORAGE/, fn -> prod(%{"SLIMSERVE_STORAGE" => "ftp"}) end
    end

    test "a local server is still the default and still needs an existing root" do
      assert prod(%{"SLIMSERVE_ROOT" => "/srv"})[:storage] in [nil, :local]

      assert_raise RuntimeError, ~r/SLIMSERVE_ROOT/, fn ->
        prod(%{"PHX_SERVER" => "true", "SLIMSERVE_ROOT" => "/nonexistent"})
      end
    end
  end

  describe "the start-up capability check" do
    @describetag :s3
    @describetag capture_log: true

    test "removes every version and upload it made, including those whose answer was lost" do
      lost = start_supervised!({Agent, fn -> %{} end})

      # The put check's first write and the complete check's upload land, but their answers never come back.
      endpoint =
        S3.proxy(fn %{method: method, path: path} ->
          kind =
            cond do
              method == "PUT" and String.ends_with?(path, "/put/key") -> :put
              method == "POST" and String.contains?(path, "/complete/key?uploads") -> :upload
              true -> nil
            end

          first? = kind && Agent.get_and_update(lost, &{not Map.has_key?(&1, kind), Map.put_new(&1, kind, path)})
          if first?, do: :drop, else: :pass
        end)

      bucket = S3.serve(endpoint: endpoint, versioned: true)
      capabilities = Capabilities.check()

      assert %{put: _put, upload: upload_path} = Agent.get(lost, & &1)
      # The lost write still occupied its key; the lost upload left completion unshown.
      assert {capabilities.put, capabilities.complete} == {true, false}
      assert S3.versions(bucket, ".slimserve-probe/") == []

      # The dropped POST's path is /<bucket>/<key>?uploads.
      key = upload_path |> URI.parse() |> Map.fetch!(:path) |> String.trim_leading("/#{bucket}/") |> URI.decode()

      assert %{status: 200, body: %{"ListMultipartUploadsResult" => uploads}} =
               S3.raw(bucket, :get, "", params: [uploads: "", prefix: key])

      refute Map.has_key?(uploads, "Upload")
    end

    test "a setup write that never landed fails the boot and records no guarantee" do
      S3.serve(endpoint: S3.proxy(fn %{method: method} -> if method == "PUT", do: {:respond, 503}, else: :pass end))

      assert Capabilities.check() == {:error, :unreachable}
      assert Capabilities.get() == %{}
      assert Capabilities.start_link() == {:error, :unreachable}
    end
  end

  describe "listing a bucket" do
    @describetag :s3

    test "follows the listing past its first page", %{conn: conn} do
      bucket = S3.serve()
      names = for n <- 1..1001, do: "f#{String.pad_leading(Integer.to_string(n), 4, "0")}.txt"
      names |> Task.async_stream(&S3.put(bucket, "many/" <> &1, "x"), max_concurrency: 32) |> Stream.run()

      assert {:ok, entries} = Storage.list(["many"], [])
      assert Enum.map(entries, & &1.name) == names
      assert conn |> get("/many/f1001.txt") |> response(200) == "x"
    end

    test "a listing read before a change is not served after it" do
      bucket = S3.serve()
      S3.put(bucket, "old.txt", "o")

      # The listing comes back, then a change lands before the fill is stored.
      S3.after_request(:get, "", fn ->
        S3.put(bucket, "new.txt", "n")
        Metadata.changed([["new.txt"]])
      end)

      assert {:ok, [%{name: "old.txt"}]} = Storage.list([], [])
      assert {:ok, [%{name: "new.txt"}, %{name: "old.txt"}]} = Storage.list([], [])
    end

    test "a fresh listing reads each folder's ignore object once" do
      bucket = S3.serve()
      for key <- ~w(d/a.txt d/b.txt d/c.txt d/sub/x.txt), do: S3.put(bucket, key, "x")
      S3.watch_requests()

      assert {:ok, [_sub, _a, _b, _c]} = Storage.list(["d"], fresh: true)

      reads =
        for {:s3_request, :get, key, _query} <- flush_requests([]), String.ends_with?(key, ".slimserveignore"), do: key

      assert Enum.frequencies(reads) == %{
               ".slimserveignore" => 1,
               "d/.slimserveignore" => 1,
               "d/sub/.slimserveignore" => 1
             }
    end

    test "reads the listed subfolders' ignore objects at the same time" do
      waiting = start_supervised!({Agent, fn -> [] end})

      endpoint =
        S3.proxy(fn %{method: method, path: path} ->
          if method == "GET" and String.match?(path, ~r{/d/s[12]/\.slimserveignore$}) do
            me = self()
            arrived = Agent.get_and_update(waiting, &{[me | &1], [me | &1]})
            if length(arrived) == 2, do: Enum.each(arrived, &send(&1, :go))
            receive(do: (:go -> :pass), after: (5_000 -> {:respond, 500}))
          else
            :pass
          end
        end)

      bucket = S3.serve(endpoint: endpoint)
      for key <- ~w(d/s1/x.txt d/s2/y.txt), do: S3.put(bucket, key, "x")

      assert {:ok, [%{name: "s1"}, %{name: "s2"}]} = Storage.list(["d"], [])
    end
  end

  describe "an open S3 page" do
    @describetag :s3

    setup do
      Application.put_env(:slimserve, :s3_poll_ms, 50)
      on_exit(fn -> Application.delete_env(:slimserve, :s3_poll_ms) end)
    end

    test "shows edits made outside the app each period, and keeps its rows while the bucket fails", %{conn: conn} do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "a.txt", "a")
      {:ok, view, _html} = live(conn, "/")

      S3.put(bucket, "b.txt", "b")
      assert eventually(view, ["/a.txt", "/b.txt"])
      S3.delete(bucket, "a.txt")
      assert eventually(view, ["/b.txt"])

      S3.watch_requests()
      Agent.update(failing, fn _ -> true end)
      S3.put(bucket, "c.txt", "c")

      for _poll <- 1..2 do
        assert_receive {:s3_request, _method, _key, _query}, 1_000
        _ = :sys.get_state(view.pid)
        flush_requests()
      end

      assert hrefs(view) == ["/b.txt"]

      Agent.update(failing, fn _ -> false end)
      assert eventually(view, ["/b.txt", "/c.txt"])
    end

    test "a folder replaced by an object of its name sends the page to the nearest folder", %{conn: conn} do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "p/x.txt", "x")
      {:ok, view, _html} = live(conn, "/p")

      # The polls fail during the swap, so none sees the folder gone before the object is there.
      Agent.update(failing, fn _ -> true end)
      S3.delete(bucket, "p/x.txt")
      S3.put(bucket, "p", "now an object")
      Agent.update(failing, fn _ -> false end)

      assert_redirect(view, "/", 1_000)
    end

    test "a store error while finding the nearest folder keeps the rows", %{conn: conn} do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "p/x.txt", "x")
      {:ok, view, _html} = live(conn, "/p")
      test = self()

      # The poll finds p gone; then the store fails and the cache is cleared, so the nearest-folder
      # search must ask the store again.
      S3.after_request(:head, "p", fn ->
        Agent.update(failing, fn _ -> true end)
        Metadata.changed([])
        send(test, :failing)
      end)

      # A poll that listed p while it still held x.txt would show it empty, so none runs across the delete.
      Agent.update(failing, fn _ -> true end)
      S3.delete(bucket, "p/x.txt")
      Agent.update(failing, fn _ -> false end)
      assert_receive :failing, 1_000
      _ = :sys.get_state(view.pid)

      assert hrefs(view) == ["/p/x.txt"]
    end
  end

  defp flush_requests(acc \\ []) do
    receive do
      {:s3_request, _method, _key, _query} = request -> flush_requests([request | acc])
    after
      0 -> Enum.reverse(acc)
    end
  end

  defp hrefs(view),
    do: view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries .name a") |> LazyHTML.attribute("href")

  defp eventually(view, expected, deadline \\ System.monotonic_time(:millisecond) + 1_000) do
    cond do
      hrefs(view) == expected ->
        true

      System.monotonic_time(:millisecond) > deadline ->
        flunk("listed #{inspect(hrefs(view))}, expected #{inspect(expected)}")

      true ->
        receive do
        after
          20 -> eventually(view, expected, deadline)
        end
    end
  end
end
