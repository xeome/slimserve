defmodule SlimServeWeb.S3IntegrationTest do
  # Not async: the storage backend and the write password are application env.
  use SlimServeWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  alias SlimServe.S3Fixture, as: S3
  alias SlimServe.Storage.S3.Capabilities
  alias Vix.Vips.Image

  @moduletag :s3

  # The spec's bound: a connected listing updates within a second.
  @within 1_000

  # A test tagged :proxied reaches MinIO through a proxy whose rule it can change: see S3Fixture.proxy/1.
  setup %{conn: conn} = tags do
    {endpoint, rules} =
      if tags[:proxied] do
        rules = start_supervised!({Agent, fn -> fn _request -> :pass end end})
        {S3.proxy(fn request -> Agent.get(rules, & &1).(request) end), rules}
      else
        {S3.endpoint(), nil}
      end

    bucket = S3.serve(endpoint: endpoint)
    {:ok, bucket: bucket, conn: conn, rules: rules}
  end

  defp hrefs(html) when is_binary(html) do
    html |> LazyHTML.from_fragment() |> LazyHTML.query("#entries .name a") |> LazyHTML.attribute("href")
  end

  defp listed(conn, path), do: conn |> get(path) |> html_response(200) |> hrefs()

  describe "browsing a bucket" do
    test "lists folders first and drops each folder's own marker", %{conn: conn, bucket: bucket} do
      S3.put(bucket, "a.txt", "alpha")
      S3.put(bucket, "a b+c.txt", "spaced")
      S3.put(bucket, "docs/", "")
      S3.put(bucket, "docs/readme.md", "readme")
      S3.put(bucket, "empty/", "")

      assert listed(conn, "/") == ["/docs", "/empty", "/a%20b%2Bc.txt", "/a.txt"]
      assert listed(conn, "/docs") == ["/docs/readme.md"]
      assert listed(conn, "/empty") == []
      assert conn |> get("/a.txt") |> response(200) == "alpha"
      assert conn |> get("/a%20b%2Bc.txt") |> response(200) == "spaced"
    end

    test "hides dot-names, ignored paths and invalid names, and refuses them by URL",
         %{conn: conn, bucket: bucket} do
      S3.put(bucket, ".slimserveignore", "secret/\n*.log\n")
      S3.put(bucket, ".env", "dot")
      S3.put(bucket, ".git/config", "dot dir")
      S3.put(bucket, "secret/x.txt", "hidden")
      S3.put(bucket, "debug.log", "hidden")
      S3.put(bucket, "keep.txt", "shown")

      assert listed(conn, "/") == ["/keep.txt"]

      for path <- ~w(/.env /.git/config /secret /secret/x.txt /debug.log /.slimserveignore /keep.txt%00 /missing.txt) do
        assert conn |> get(path) |> response(404), path
      end

      # %2F decodes into one segment, which is never a key.
      S3.put(bucket, "d/e.txt", "e")
      assert conn |> get("/d%2Fe.txt") |> response(404)
    end

    # MinIO cannot hold both, so the listing that has both is stubbed.
    @tag :proxied
    test "a name that is both an object and a prefix is listed once, as a folder", %{conn: conn, rules: rules} do
      listing = """
      <ListBucketResult><IsTruncated>false</IsTruncated>\
      <Contents><Key>both</Key><ETag>"e1"</ETag><Size>1</Size><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>\
      <Contents><Key>file.txt</Key><ETag>"e2"</ETag><Size>2</Size><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>\
      <CommonPrefixes><Prefix>both%2F</Prefix></CommonPrefixes></ListBucketResult>
      """

      Agent.update(rules, fn _rule ->
        fn request -> if request.path =~ "delimiter=%2F", do: {:respond, 200, listing}, else: :pass end
      end)

      html = conn |> get("/") |> html_response(200)
      assert hrefs(html) == ["/both", "/file.txt"]
      assert html |> LazyHTML.from_fragment() |> LazyHTML.query("#entries .kind.dir") |> Enum.count() == 1
    end

    test "an ignore file that is not valid UTF-8 hides its folder", %{conn: conn, bucket: bucket} do
      S3.put(bucket, "d/.slimserveignore", <<0xFF, 0xFE>>)
      S3.put(bucket, "d/x.txt", "x")
      S3.put(bucket, "y.txt", "y")

      assert listed(conn, "/") == ["/y.txt"]
      assert conn |> get("/d/x.txt") |> response(404)
    end
  end

  describe "writing to a bucket" do
    # A MinIO that ignores a stale DELETE If-Match logs that move and delete check for themselves.
    @describetag capture_log: true

    setup %{conn: conn} do
      Application.put_env(:slimserve, :write_password, "wpass")
      on_exit(fn -> for key <- [:write_password, :upload_types], do: Application.delete_env(:slimserve, key) end)

      # A fresh /64 per test, so no test's attempts reach another's rate limit.
      conn = %{conn | remote_ip: {0xFD02, 0, 0, rem(System.unique_integer([:positive]), 65_536), 0, 0, 0, 1}}
      {:ok, writer: conn |> post("/.app/login", %{"password" => "wpass"}) |> recycle()}
    end

    test "a writer uploads, makes a folder, moves a file there and back and deletes it, and another page follows",
         %{writer: writer, bucket: bucket} do
      assert %{put: true, mkdir: true} = Capabilities.check()
      {:ok, view, _html} = live(writer, "/")
      {:ok, other, _html} = live(writer, "/")

      upload(view, "a.txt", "hello")
      assert S3.objects(bucket) == %{"a.txt" => "hello"}
      assert_listed(other, ["/a.txt"])

      view |> element("#mkdir-form") |> render_submit(%{"name" => "new"})
      assert S3.objects(bucket) == %{"a.txt" => "hello", "new/" => ""}
      assert_listed(other, ["/new", "/a.txt"])

      rename(view, "a.txt", "new/b.txt")
      assert S3.objects(bucket) == %{"new/" => "", "new/b.txt" => "hello"}
      assert_listed(other, ["/new"])

      {:ok, in_new, _html} = live(writer, "/new")
      rename(in_new, "new/b.txt", "a.txt")
      assert S3.objects(bucket) == %{"new/" => "", "a.txt" => "hello"}
      assert_listed(other, ["/new", "/a.txt"])

      remove(view, "a.txt")
      assert S3.objects(bucket) == %{"new/" => ""}
      assert_listed(other, ["/new"])
      refute has_element?(view, "#flash-error")
    end

    test "an upload or a new folder onto a name taken after the fresh check is refused, and the winner stays",
         %{writer: writer, bucket: bucket} do
      Capabilities.check()
      {:ok, view, _html} = live(writer, "/")

      # The fresh check's last question is a HEAD of the exact key; the winner lands right after it.
      S3.after_request(:head, "race.txt", fn -> S3.put(bucket, "race.txt", "winner") end)
      upload(view, "race.txt", "loser")
      assert has_element?(view, "#flash-error")

      S3.after_request(:head, "racedir", fn -> S3.put(bucket, "racedir/", "winner marker") end)
      view |> element("#mkdir-form") |> render_submit(%{"name" => "racedir"})

      assert S3.objects(bucket) == %{"race.txt" => "winner", "racedir/" => "winner marker"}
    end

    test "a folder moves key by key to the exact new keys, markers and dot-files included",
         %{writer: writer, bucket: bucket} do
      Capabilities.check()
      seed = %{"c/" => "", "c/d/" => "", "c/d/x.txt" => "x", "c/.env" => "secret", "c/sp ace/y+z.txt" => "yz"}
      for {key, body} <- seed, do: S3.put(bucket, key, body)
      S3.put(bucket, "cc/keep.txt", "k")
      {:ok, view, _html} = live(writer, "/")

      # Inside itself is refused; a sibling that only shares the leading characters is not inside.
      rename(view, "c", "c/inner")
      assert has_element?(view, "#flash-error")
      rename(view, "c", "cc/c")

      moved = Map.new(seed, fn {"c/" <> rest, body} -> {"cc/c/" <> rest, body} end)
      assert S3.objects(bucket) == Map.put(moved, "cc/keep.txt", "k")
    end

    test "a folder holding a file its ignore rules hide does not move, even when the cached rules are older",
         %{writer: writer, bucket: bucket} do
      Capabilities.check()
      S3.put(bucket, "a/deep/secret.txt", "s")
      {:ok, view, _html} = live(writer, "/")
      assert hrefs(render(view)) == ["/a"]

      # The listing cached the root's rules as none; the move must read them again.
      S3.put(bucket, ".slimserveignore", "/a/deep/secret.txt\n")
      before = S3.objects(bucket)
      rename(view, "a", "b")
      assert S3.objects(bucket) == before
    end

    test "a move uses the entry's kind now, not the kind the page listed", %{writer: writer, bucket: bucket} do
      Capabilities.check()
      Application.put_env(:slimserve, :upload_types, [".txt"])
      S3.put(bucket, "k", "a file")
      {:ok, view, _html} = live(writer, "/")

      # Listed as a file, which may not take a name off the list; now a folder, which moves to any name.
      S3.delete(bucket, "k")
      S3.put(bucket, "k/inner.txt", "i")
      rename(view, "k", "k2")
      assert S3.objects(bucket) == %{"k2/inner.txt" => "i"}
    end

    test "a folder with files is deleted only after the bar that counts them, and asks again after a change",
         %{writer: writer, bucket: bucket} do
      Capabilities.check()
      S3.put(bucket, "sub/", "")
      S3.put(bucket, "sub/x.txt", "x")
      S3.put(bucket, "sub/deep/.y", "y")
      S3.put(bucket, "empty/", "")
      {:ok, view, _html} = live(writer, "/")

      remove(view, "empty")
      refute Map.has_key?(S3.objects(bucket), "empty/")

      remove(view, "sub")
      assert has_element?(view, "#confirm-delete[data-count='2']")
      S3.put(bucket, "sub/z.txt", "z")
      view |> element("#confirm-delete-yes") |> render_click()
      assert has_element?(view, "#confirm-delete[data-count='3']")
      assert map_size(S3.objects(bucket)) == 4

      view |> element("#confirm-delete-yes") |> render_click()
      assert S3.objects(bucket) == %{}
    end

    test "a folder holding only empty folders is deleted only after the bar", %{writer: writer, bucket: bucket} do
      Capabilities.check()
      S3.put(bucket, "sub/", "")
      S3.put(bucket, "sub/deep/", "")
      {:ok, view, _html} = live(writer, "/")

      remove(view, "sub")
      assert has_element?(view, "#confirm-delete")
      assert S3.objects(bucket) == %{"sub/" => "", "sub/deep/" => ""}
    end

    @tag :proxied
    test "a folder delete that fails for one key reports a partial outcome and keeps that key",
         %{writer: writer, bucket: bucket, rules: rules} do
      Capabilities.check()
      for key <- ~w(f/a.txt f/b.txt f/c.txt), do: S3.put(bucket, key, key)
      {:ok, view, _html} = live(writer, "/")
      remove(view, "f")

      Agent.update(rules, fn _rule ->
        fn request -> if request.method == "DELETE" and request.path =~ "/f/b.txt", do: {:respond, 500}, else: :pass end
      end)

      view |> element("#confirm-delete-yes") |> render_click()
      assert S3.objects(bucket) == %{"f/b.txt" => "f/b.txt"}
      assert view |> element("#flash-error") |> render() =~ "Part of that change was refused."
    end

    # A provider that answers 200 to a conditional write it did not honour fails the check.
    @tag :proxied
    @tag capture_log: true
    test "without the no-clobber guarantees, upload and new folder are refused, and move and delete still work",
         %{writer: writer, bucket: bucket, rules: rules} do
      Agent.update(rules, fn _rule ->
        fn request ->
          if request.method == "PUT" and List.keymember?(request.headers, "if-none-match", 0),
            do: {:respond, 200},
            else: :pass
        end
      end)

      assert %{put: false, mkdir: false, slash_put: false} = Capabilities.check()
      # From here every write reaches MinIO, so only the app's own refusal can keep the bucket unchanged.
      Agent.update(rules, fn _rule -> fn _request -> :pass end end)
      S3.put(bucket, "m.txt", "m")
      S3.put(bucket, "e.txt", "")
      {:ok, view, _html} = live(writer, "/")

      upload(view, "u.txt", "u")
      assert has_element?(view, "#flash-error")
      view |> element("#mkdir-form") |> render_submit(%{"name" => "dir"})
      assert S3.objects(bucket) == %{"m.txt" => "m", "e.txt" => ""}

      rename(view, "m.txt", "moved.txt")
      rename(view, "e.txt", "e2.txt")
      remove(view, "moved.txt")
      assert S3.objects(bucket) == %{"e2.txt" => ""}
    end

    @tag :tmp_dir
    test "a replaced image gets a new thumbnail URL and the new thumbnail",
         %{writer: writer, conn: conn, bucket: bucket, tmp_dir: tmp_dir} do
      # An empty thumbnail cache, so the first request can only miss.
      :ok = Supervisor.terminate_child(SlimServe.Supervisor, SlimServe.ThumbCache)
      on_exit(fn -> {:ok, _pid} = Supervisor.restart_child(SlimServe.Supervisor, SlimServe.ThumbCache) end)
      start_supervised!({SlimServe.ThumbCache, dir: tmp_dir, max_bytes: 1024 * 1024})
      Capabilities.check()
      S3.put(bucket, "p.png", png([200, 0, 0]))
      {:ok, view, _html} = live(writer, "/")
      [old_thumb] = thumbs(view)
      assert %{status: 200} = red = get(conn, old_thumb)
      assert get_resp_header(red, "x-cache") == ["miss"]
      # The cache stores with a cast.
      _ = :sys.get_state(SlimServe.ThumbCache)
      assert get_resp_header(get(conn, old_thumb), "x-cache") == ["hit"]

      remove(view, "p.png")
      upload(view, "p.png", png([0, 0, 200]))
      [new_thumb] = eventually(fn -> thumbs(view) end, &(&1 not in [[], [old_thumb]]))
      blue = get(conn, new_thumb)
      assert get_resp_header(blue, "x-cache") == ["miss"]
      assert pixel(blue.resp_body) != pixel(red.resp_body)
    end
  end

  defp png(colour), do: 40 |> Image.build_image!(40, colour) |> Image.write_to_buffer(".png") |> elem(1)

  defp pixel(webp) do
    {:ok, image} = Image.new_from_buffer(webp)
    Vix.Vips.Operation.getpoint!(image, 1, 1)
  end

  defp thumbs(view),
    do: view |> render() |> LazyHTML.from_fragment() |> LazyHTML.query("#entries img.thumb") |> LazyHTML.attribute("src")

  defp upload(view, name, content) do
    input = file_input(view, "#upload-form", :files, [%{name: name, content: content, type: MIME.from_path(name)}])
    render_upload(input, name)
    view |> element("#upload-form") |> render_submit()
  end

  defp rename(view, from, to), do: render_hook(view, "rename", %{"path" => "/" <> from, "to" => to})
  defp remove(view, path), do: render_hook(view, "delete", %{"path" => "/" <> path})

  defp assert_listed(view, expected), do: eventually(fn -> hrefs(render(view)) end, &(&1 == expected))

  defp eventually(read, done?, deadline \\ System.monotonic_time(:millisecond) + @within) do
    value = read.()

    cond do
      done?.(value) ->
        value

      System.monotonic_time(:millisecond) > deadline ->
        flunk("still #{inspect(value)} after #{@within} ms")

      true ->
        receive do
        after
          20 -> eventually(read, done?, deadline)
        end
    end
  end
end
