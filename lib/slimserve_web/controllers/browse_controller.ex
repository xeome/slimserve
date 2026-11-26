defmodule SlimServeWeb.BrowseController do
  use SlimServeWeb, :controller

  import Phoenix.LiveView.Controller, only: [live_render: 3]

  alias SlimServe.Storage
  alias SlimServe.Storage.S3.Download
  alias SlimServe.ThumbCache
  alias SlimServe.Thumbnail
  alias SlimServeWeb.BrowseHTML
  alias SlimServeWeb.ListingLive

  # A thumbnail goes through the same gate as a download, so a hidden image has none either.
  # A local file's is keyed by its stat; an S3 object's by its key and ETag, the listing's ?v=.
  def show(conn, %{"path" => segments, "thumb" => "1"}) do
    thumb(conn, Storage.resolve(segments), Application.fetch_env!(:slimserve, :thumb_max_dim))
  end

  def show(conn, %{"path" => segments}) do
    case Storage.resolve(segments) do
      {:dir, _dir} ->
        listing(conn, segments)

      # A file removed between resolve and stat is missing like any other.
      {:file, file} when is_binary(file) ->
        case File.stat(file) do
          {:ok, stat} -> download(conn, file, List.last(segments), stat.size)
          {:error, _reason} -> BrowseHTML.send_not_found(conn)
        end

      {:file, object} ->
        download(conn, object, List.last(segments), object.size)

      # The store could not be asked: not a missing path.
      {:error, _reason} ->
        send_resp(conn, 502, "Bad Gateway")

      :error ->
        BrowseHTML.send_not_found(conn)
    end
  end

  defp thumb(conn, {:file, file}, max_dim) when is_binary(file) do
    generate = fn ->
      cond do
        conn.private[:head?] != true -> Thumbnail.generate(file, max_dim)
        Thumbnail.loadable?(file) -> :head
        true -> {:error, :not_loadable}
      end
    end

    with true <- Thumbnail.image?(file),
         {:ok, stat} <- File.stat(file, time: :posix) do
      thumbnail(conn, stat.size, ThumbCache.key(file, stat, max_dim), generate)
    else
      _ -> BrowseHTML.send_not_found(conn)
    end
  end

  defp thumb(conn, {:file, %{key: key, etag: etag} = object}, max_dim) do
    generate = fn -> Download.with_file(object, &Thumbnail.generate(&1, max_dim), conn.private[:head?] == true) end

    if Thumbnail.image?(key) do
      thumbnail(conn, object.size, ThumbCache.key(key, etag, max_dim), generate)
    else
      BrowseHTML.send_not_found(conn)
    end
  end

  defp thumb(conn, {:error, _reason}, _max_dim), do: send_resp(conn, 502, "Bad Gateway")
  defp thumb(conn, _missing, _max_dim), do: BrowseHTML.send_not_found(conn)

  defp listing(conn, segments) do
    case conn.params do
      %{"view" => view} when view in ~w(grid list) ->
        conn
        |> put_resp_cookie("slimserve-view", view, max_age: 31_536_000, same_site: "Lax", http_only: true)
        |> redirect(to: ListingLive.href(segments))

      _ ->
        view = if conn.cookies["slimserve-view"] == "grid", do: "grid", else: "list"
        # Written by the sidebar's hook; read here so the first paint already has the rail right.
        rail = if conn.cookies["slimserve-rail"] in ~w(rail open), do: conn.cookies["slimserve-rail"]
        live_render(conn, ListingLive, session: %{"segments" => segments, "view" => view, "rail" => rail})
    end
  end

  # The path comes only from Storage.resolve; the type is served with nosniff and the default-src 'self' CSP.
  # The type comes from the requested name: a link named x.txt is text, whatever it points to.
  # sobelow_skip ["Traversal.SendFile", "XSS.ContentType"]
  defp download(conn, file, name, size) do
    conn =
      conn
      |> put_resp_content_type(MIME.from_path(name), nil)
      |> put_resp_header("accept-ranges", "bytes")

    case byte_range(get_req_header(conn, "range"), size) do
      {first, last} ->
        conn
        |> put_resp_header("content-range", "bytes #{first}-#{last}/#{size}")
        |> send_bytes(206, file, first, last - first + 1)

      :unsatisfiable ->
        conn |> put_resp_header("content-range", "bytes */#{size}") |> send_resp(416, "")

      :full ->
        send_bytes(conn, 200, file, 0, :all)
    end
  end

  # sobelow_skip ["Traversal.SendFile"]
  defp send_bytes(conn, status, file, offset, length) when is_binary(file),
    do: send_file(conn, status, file, offset, length)

  defp send_bytes(conn, status, object, offset, length), do: Download.send(conn, status, object, offset, length)

  defp byte_range(_headers, 0), do: :full

  defp byte_range([header], size) do
    case Regex.run(~r/\Abytes=(\d*)-(\d*)\z/i, header, capture: :all_but_first) do
      [first, last] -> span(to_int(first), to_int(last), size)
      nil -> :full
    end
  end

  defp byte_range(_headers, _size), do: :full

  defp to_int(""), do: nil
  defp to_int(digits), do: String.to_integer(digits)

  defp span(nil, nil, _size), do: :full
  defp span(nil, 0, _size), do: :unsatisfiable
  defp span(nil, n, size), do: {max(size - n, 0), size - 1}
  defp span(first, last, _size) when is_integer(last) and last < first, do: :full
  defp span(first, _last, size) when first >= size, do: :unsatisfiable
  defp span(first, last, size), do: {first, min(last || size - 1, size - 1)}

  defp thumbnail(conn, size, key, generate) do
    # The size is checked before libvips opens the file. A file libvips cannot decode is not an image.
    with false <- size > Application.fetch_env!(:slimserve, :thumb_max_file_bytes),
         :miss <- ThumbCache.lookup(key),
         # ponytail: no concurrency limit; add a bounded pool if many viewers hit a cold cache.
         {:ok, webp, dims} <- generate.() do
      # A cast: the cache logs a failed write, and it only costs the next request a miss.
      ThumbCache.put(ThumbCache, key, webp, dims)
      send_webp(conn, "miss", webp)
    else
      true -> send_resp(conn, 413, "Content Too Large")
      {:ok, webp} -> send_webp(conn, "hit", webp)
      # Bandit adds no content-length to an empty HEAD answer, so the client is told no length, not 0.
      :head -> send_webp(conn, "miss", "")
      {:error, :unavailable} -> send_resp(conn, 503, "Service Unavailable")
      {:error, _reason} -> BrowseHTML.send_not_found(conn)
    end
  end

  defp send_webp(conn, cache, webp) do
    conn |> put_resp_content_type("image/webp", nil) |> put_resp_header("x-cache", cache) |> send_resp(200, webp)
  end
end
