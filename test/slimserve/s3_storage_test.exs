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

    test "Scenario: An S3 server needs no root, and takes its bucket and the standard AWS variables" do
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

    test "Scenario: An empty download cache directory takes the default" do
      assert prod(Map.put(@s3, "SLIMSERVE_DOWNLOAD_CACHE_DIR", ""))[:download_cache_dir] ==
               Path.join(System.tmp_dir!(), "slimserve-downloads")
    end

    test "Scenario: An S3 server without a bucket or credentials, or an unknown backend, refuses to boot" do
      assert_raise RuntimeError, ~r/SLIMSERVE_S3_BUCKET/, fn -> prod(Map.delete(@s3, "SLIMSERVE_S3_BUCKET")) end
      assert_raise RuntimeError, ~r/AWS_SECRET_ACCESS_KEY/, fn -> prod(Map.delete(@s3, "AWS_SECRET_ACCESS_KEY")) end
      assert_raise RuntimeError, ~r/SLIMSERVE_STORAGE/, fn -> prod(%{"SLIMSERVE_STORAGE" => "ftp"}) end
    end

    test "Scenario: A local server is still the default and still needs an existing root" do
      assert prod(%{"SLIMSERVE_ROOT" => "/srv"})[:storage] in [nil, :local]

      assert_raise RuntimeError, ~r/SLIMSERVE_ROOT/, fn ->
        prod(%{"PHX_SERVER" => "true", "SLIMSERVE_ROOT" => "/nonexistent"})
      end
    end
  end

  describe "the start-up capability check" do
    @describetag :s3
    @describetag capture_log: true

    test "Scenario: The start-up check removes every version and upload it made, including those whose answer was lost" do
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

    test "Scenario: A start-up check write that never lands fails the boot" do
      S3.serve(endpoint: S3.proxy(fn %{method: method} -> if method == "PUT", do: {:respond, 503}, else: :pass end))

      assert Capabilities.check() == {:error, :unreachable}
      assert Capabilities.get() == %{}
      assert Capabilities.start_link() == {:error, :unreachable}
    end
  end

  describe "listing a bucket" do
    @describetag :s3

    test "Scenario: A bucket listing follows the store past its first page", %{conn: conn} do
      bucket = S3.serve()
      names = for n <- 1..1001, do: "f#{String.pad_leading(Integer.to_string(n), 4, "0")}.txt"
      names |> Task.async_stream(&S3.put(bucket, "many/" <> &1, "x"), max_concurrency: 32) |> Stream.run()

      assert {:ok, entries} = Storage.list(["many"])
      assert Enum.map(entries, & &1.name) == names
      assert conn |> get("/many/f1001.txt") |> response(200) == "x"
    end

    test "Scenario: A listing read before a change is not served after it" do
      bucket = S3.serve()
      S3.put(bucket, "old.txt", "o")

      # The listing comes back, then a change lands before the fill is stored.
      S3.after_request(:get, "", fn ->
        S3.put(bucket, "new.txt", "n")
        Metadata.changed([["new.txt"]], [], [])
      end)

      assert {:ok, [%{name: "old.txt"}]} = Storage.list([])
      assert {:ok, [%{name: "new.txt"}, %{name: "old.txt"}]} = Storage.list([])
    end

    test "Scenario: A fresh listing reads each folder's ignore object once" do
      bucket = S3.serve()
      for key <- ~w(d/a.txt d/b.txt d/c.txt d/sub/x.txt), do: S3.put(bucket, key, "x")
      S3.watch_requests()

      assert {:ok, [_sub, _a, _b, _c]} = Storage.list(["d"])

      reads =
        for {:s3_request, :get, key, _query} <- flush_requests(), String.ends_with?(key, ".slimserveignore"), do: key

      assert Enum.frequencies(reads) == %{
               ".slimserveignore" => 1,
               "d/.slimserveignore" => 1,
               "d/sub/.slimserveignore" => 1
             }
    end

    test "Scenario: The bucket gets both subfolders' ignore-object reads before it answers either" do
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

      assert {:ok, [%{name: "s1"}, %{name: "s2"}]} = Storage.list(["d"])
    end
  end

  describe "the metadata cache" do
    @describetag :s3
    @describetag :tmp_dir

    test "Scenario: After an upload, another folder lists again with no request to the bucket", %{tmp_dir: tmp_dir} do
      bucket = S3.serve()
      for key <- ~w(a/x.txt b/y.txt), do: S3.put(bucket, key, "x")
      Capabilities.check()
      assert {:ok, [%{name: "y.txt"}]} = Storage.list(["b"])
      assert {:file, _object} = Storage.resolve(["b", "y.txt"])

      assert upload(tmp_dir, ["a"], "n.txt") == :ok
      S3.watch_callers()
      assert {:ok, [%{name: "y.txt"}]} = Storage.list(["b"])
      assert {:file, _object} = Storage.resolve(["b", "y.txt"])
      test = self()
      assert for({:s3_call, callers, _method, _key, _query} <- flush_calls(), test in callers, do: callers) == []

      assert {:ok, {:ok, {[%{key: "b/y.txt"}], []}}} = cached({:list, "b/"})
      assert {:ok, {:file, %{key: "b/y.txt"}}} = cached({:stat, "b/y.txt"})
    end

    test "Scenario: An upload asks the store again for its own folder's listing only", %{tmp_dir: tmp_dir} do
      bucket = S3.serve()
      S3.put(bucket, "a/b/f.txt", "f")
      Capabilities.check()
      for path <- [[], ["a"], ["a", "b"]], do: assert({:ok, [_entry]} = Storage.list(path))
      ancestors = for key <- [{:list, ""}, {:list, "a/"}, {:folder, "a"}, {:folder, "a/b"}], do: {key, cached(key)}
      assert Enum.all?(ancestors, &match?({_key, {:ok, _value}}, &1))
      S3.watch_callers()
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, SlimServe.Root.topic("a/b"))

      assert upload(tmp_dir, ["a", "b"], "g.txt") == :ok

      assert_receive {:dir_changed, "a/b"}, 1_000
      metadata = Process.whereis(Metadata)

      assert [{:get, "", query}] =
               for(
                 {:s3_call, callers, method, key, query} <- flush_calls(),
                 metadata in callers,
                 do: {method, key, query}
               )

      assert {query[:prefix], query[:delimiter]} == {"a/b/", "/"}
      assert ancestors == for({key, _value} <- ancestors, do: {key, cached(key)})
    end

    test "Scenario: A delete that empties an implicit folder leaves it absent with no further request" do
      bucket = S3.serve()
      for key <- ~w(a/keep.txt a/b/f.txt), do: S3.put(bucket, key, "x")
      assert {:ok, [%{name: "b"}, %{name: "keep.txt"}]} = Storage.list(["a"])
      assert {:dir, "a/b/"} = Storage.resolve(["a", "b"])
      assert {:ok, {:ok, {_objects, ["a/b/"]}}} = cached({:list, "a/"})
      assert cached({:folder, "a/b"}) == {:ok, {:ok, true}}
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, SlimServe.Root.topic("a"))

      assert Storage.delete(["a", "b"], "f.txt") == :ok

      assert_receive {:dir_changed, "a"}, 1_000
      assert {:ok, {:ok, {_objects, []}}} = cached({:list, "a/"})
      assert cached({:folder, "a/b"}) == {:ok, {:ok, false}}
      S3.watch_callers()
      assert {:ok, [%{name: "keep.txt"}]} = Storage.list(["a"])
      test = self()
      assert for({:s3_call, callers, _method, _key, _query} <- flush_calls(), test in callers, do: callers) == []
    end

    test "Scenario: A folder emptied just before an upload into it lists again once the upload lands", %{tmp_dir: tmp_dir} do
      bucket = S3.serve()
      S3.put(bucket, "a/b/f.txt", "f")
      Capabilities.check()
      for path <- [[], ["a"], ["a", "b"]], do: assert({:ok, [_entry]} = Storage.list(path))

      # The upload's last check before its PUT.
      S3.after_request(:head, "a/b/g.txt", fn -> :ok = Storage.delete(["a", "b"], "f.txt") end)
      assert upload(tmp_dir, ["a", "b"], "g.txt") == :ok

      assert {:ok, [%{name: "b"}]} = Storage.list(["a"])
      assert {:dir, _prefix} = Storage.resolve(["a", "b"])
    end

    test "Scenario: A folder move makes the names under its destination resolve" do
      bucket = S3.serve()
      S3.put(bucket, "src/f.txt", "f")
      Capabilities.check()
      assert Storage.resolve(["dst", "f.txt"]) == :error
      assert cached({:stat, "dst/f.txt"}) == {:ok, :error}

      assert Storage.move([], "src", "dst") == :ok

      assert {:file, %{key: "dst/f.txt"}} = Storage.resolve(["dst", "f.txt"])
    end

    test "Scenario: After an upload wakes a page, its first listing shows the new file with no request to the bucket", %{
      tmp_dir: tmp_dir
    } do
      bucket = S3.serve()
      S3.put(bucket, "a/x.txt", "x")
      Capabilities.check()
      assert {:ok, [_entry]} = Storage.list(["a"])
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, SlimServe.Root.topic("a"))
      S3.watch_callers()

      assert upload(tmp_dir, ["a"], "n.txt") == :ok

      metadata = Process.whereis(Metadata)
      assert Enum.any?(until_woken("a"), &list_call?(&1, metadata, "a/"))
      assert {:ok, [%{name: "n.txt"}, %{name: "x.txt"}]} = Storage.list(["a"])
      test = self()
      assert for({:s3_call, callers, _method, _key, _query} <- flush_calls(), test in callers, do: callers) == []
    end

    test "Scenario: A burst of uploads into one folder gets one listing request, and the open page shows all three", %{
      conn: conn,
      tmp_dir: tmp_dir
    } do
      bucket = S3.serve()
      S3.put(bucket, "a/x.txt", "x")
      Capabilities.check()
      {:ok, view, _html} = live(conn, "/a")
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, SlimServe.Root.topic("a"))
      S3.watch_callers()
      names = ~w(n1.txt n2.txt n3.txt)

      assert names |> Task.async_stream(&upload(tmp_dir, ["a"], &1)) |> Enum.to_list() == [ok: :ok, ok: :ok, ok: :ok]

      woken = until_woken("a")
      refute_receive {:dir_changed, "a"}, 300
      assert eventually(view, ["/a/n1.txt", "/a/n2.txt", "/a/n3.txt", "/a/x.txt"])
      _ = :sys.get_state(view.pid)
      calls = woken ++ flush_calls()

      metadata = Process.whereis(Metadata)
      assert Enum.count(calls, &list_call?(&1, metadata, "a/")) == 1
      assert for({:s3_call, callers, method, key, _query} <- calls, view.pid in callers, do: {method, key}) == []
    end

    @tag capture_log: true
    test "Scenario: After a malformed listing answer, the folder is listed from the store again and the server keeps serving",
         %{tmp_dir: tmp_dir} do
      malformed = start_supervised!({Agent, fn -> false end})

      body =
        ~s(<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Contents><Key>a/x.txt</Key></Contents></ListBucketResult>)

      endpoint =
        S3.proxy(fn %{method: method, path: path} ->
          query = URI.decode_query(URI.parse(path).query || "")
          refill? = method == "GET" and query["prefix"] == "a/" and query["delimiter"] == "/"
          if refill? and Agent.get_and_update(malformed, &{&1, false}), do: {:respond, 200, body}, else: :pass
        end)

      bucket = S3.serve(endpoint: endpoint)
      S3.put(bucket, "a/x.txt", "x")
      Capabilities.check()
      assert {:ok, [_entry]} = Storage.list(["a"])
      metadata = Process.whereis(Metadata)
      :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, SlimServe.Root.topic("a"))
      Agent.update(malformed, fn _ -> true end)

      assert upload(tmp_dir, ["a"], "n.txt") == :ok

      assert_receive {:dir_changed, "a"}, 1_000
      refute Agent.get(malformed, & &1)
      assert Process.whereis(Metadata) == metadata
      assert :ets.info(Metadata) != :undefined
      assert cached({:list, "a/"}) == :miss
      S3.watch_callers()
      assert {:ok, [%{name: "n.txt"}, %{name: "x.txt"}]} = Storage.list(["a"])
      assert Enum.any?(flush_calls(), &list_call?(&1, self(), "a/"))
    end
  end

  describe "an open S3 page" do
    @describetag :s3

    @tag :tmp_dir
    test "Scenario: An open bucket page shows a write and adds no request to the bucket", %{conn: conn, tmp_dir: tmp_dir} do
      bucket = S3.serve()
      for key <- ~w(a/x.txt a/sub/y.txt z/w.txt), do: S3.put(bucket, key, "x")
      Capabilities.check()
      {:ok, view, _html} = live(conn, "/a")
      S3.watch_callers()

      assert upload(tmp_dir, ["a"], "n.txt") == :ok

      assert eventually(view, ["/a/sub", "/a/n.txt", "/a/x.txt"])
      _ = :sys.get_state(view.pid)
      assert for({:s3_call, callers, method, key, _query} <- flush_calls(), view.pid in callers, do: {method, key}) == []
    end

    test "Scenario: While a bucket page sits open and idle, the bucket gets no request", %{conn: conn} do
      Application.put_env(:slimserve, :s3_poll_ms, 50)
      on_exit(fn -> Application.delete_env(:slimserve, :s3_poll_ms) end)
      bucket = S3.serve()
      S3.put(bucket, "a.txt", "a")
      {:ok, view, _html} = live(conn, "/")

      # An empty cache, so a leftover re-list would have to reach the store.
      stop_supervised!(Metadata)
      start_supervised!(Metadata)
      S3.watch_requests()

      refute_receive {:s3_request, _method, _key, _query}, 300
      _ = :sys.get_state(view.pid)
      refute_receive {:s3_request, _method, _key, _query}
    end

    @tag :tmp_dir
    @tag capture_log: true
    test "Scenario: An open bucket page shows an upload whose answer was lost", %{conn: conn, tmp_dir: tmp_dir} do
      endpoint =
        S3.proxy(fn %{method: method, path: path} ->
          if method == "PUT" and String.ends_with?(path, "/n.txt"), do: :drop, else: :pass
        end)

      bucket = S3.serve(endpoint: endpoint)
      S3.put(bucket, "a.txt", "a")
      Capabilities.check()
      {:ok, view, _html} = live(conn, "/")

      assert upload(tmp_dir, [], "n.txt") == :error

      assert eventually(view, ["/a.txt", "/n.txt"])
      assert {:ok, [%{name: "a.txt"}, %{name: "n.txt"}]} = Storage.list([])
    end

    @tag capture_log: true
    test "Scenario: An open bucket page drops a file whose delete's answer was lost", %{conn: conn} do
      endpoint =
        S3.proxy(fn %{method: method, path: path} ->
          if method == "DELETE" and String.ends_with?(path, "/a.txt"), do: :drop, else: :pass
        end)

      bucket = S3.serve(endpoint: endpoint)
      for key <- ~w(a.txt b.txt), do: S3.put(bucket, key, "x")
      {:ok, view, _html} = live(conn, "/")

      assert Storage.delete([], "a.txt") == :error

      assert eventually(view, ["/b.txt"])
      assert {:ok, [%{name: "b.txt"}]} = Storage.list([])
    end

    @tag :tmp_dir
    test "Scenario: An open bucket page keeps its rows while the bucket fails", %{conn: conn, tmp_dir: tmp_dir} do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "b.txt", "b")
      Capabilities.check()
      {:ok, view, _html} = live(conn, "/")

      S3.after_request(:put, "c.txt", fn -> Agent.update(failing, fn _ -> true end) end)
      S3.watch_callers()
      assert upload(tmp_dir, [], "c.txt") == :ok

      await_list(view.pid, "")
      _ = :sys.get_state(view.pid)
      assert hrefs(view) == ["/b.txt"]
    end

    @tag :tmp_dir
    test "Scenario: An open bucket page lists a folder it could not read again once the store recovers", %{
      conn: conn,
      tmp_dir: tmp_dir
    } do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "b.txt", "b")
      Capabilities.check()
      {:ok, view, _html} = live(conn, "/")
      S3.after_request(:put, "c.txt", fn -> Agent.update(failing, fn _ -> true end) end)
      S3.watch_callers()
      assert upload(tmp_dir, [], "c.txt") == :ok

      await_list(view.pid, "")
      Agent.update(failing, fn _ -> false end)

      assert eventually(view, ["/b.txt", "/c.txt"])
    end

    test "Scenario: A delete of the open bucket folder sends the page to the nearest folder", %{conn: conn} do
      bucket = S3.serve()
      S3.put(bucket, "p/x.txt", "x")
      {:ok, view, _html} = live(conn, "/p")

      assert {:confirm, asked} = Storage.delete([], "p")
      assert Storage.confirm_delete([], "p", asked) == :ok

      assert_redirect(view, "/", 1_000)
    end

    test "Scenario: A store error while finding the nearest folder keeps the rows", %{conn: conn} do
      failing = start_supervised!({Agent, fn -> false end})

      bucket =
        S3.serve(endpoint: S3.proxy(fn _request -> if Agent.get(failing, & &1), do: {:respond, 500}, else: :pass end))

      S3.put(bucket, "p/x.txt", "x")
      {:ok, view, _html} = live(conn, "/p")
      test = self()

      S3.after_request(:head, "p", fn ->
        Agent.update(failing, fn _ -> true end)
        :ok = Metadata.changed([], [["p"]], [])
        send(test, :failing)
      end)

      assert Storage.delete(["p"], "x.txt") == :ok
      assert_receive :failing, 1_000
      _ = :sys.get_state(view.pid)

      assert hrefs(view) == ["/p/x.txt"]
    end
  end

  defp upload(tmp_dir, segments, name) do
    file = Path.join(tmp_dir, name)
    File.write!(file, name)
    Storage.upload(segments, name, file)
  end

  defp cached(key) do
    case :ets.lookup(Metadata, key) do
      [{^key, value, _expires}] -> {:ok, value}
      [] -> :miss
    end
  end

  defp list_call?({:s3_call, callers, :get, "", query}, pid, prefix),
    do: pid in callers and query[:prefix] == prefix and query[:delimiter] == "/"

  defp list_call?(_call, _pid, _prefix), do: false

  defp until_woken(dir, calls \\ []) do
    receive do
      {:dir_changed, ^dir} -> Enum.reverse(calls)
      {:s3_call, _callers, _method, _key, _query} = call -> until_woken(dir, [call | calls])
    after
      1_000 -> flunk("#{dir} was never woken")
    end
  end

  defp await_list(pid, prefix) do
    receive do
      {:s3_call, _callers, _method, _key, _query} = call ->
        if list_call?(call, pid, prefix), do: :ok, else: await_list(pid, prefix)
    after
      1_000 -> flunk("the page never listed #{inspect(prefix)}")
    end
  end

  defp flush_calls(acc \\ []) do
    receive do
      {:s3_call, _callers, _method, _key, _query} = call -> flush_calls([call | acc])
    after
      0 -> Enum.reverse(acc)
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
