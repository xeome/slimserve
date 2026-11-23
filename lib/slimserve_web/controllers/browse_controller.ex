defmodule SlimServeWeb.BrowseController do
  use SlimServeWeb, :controller

  import Phoenix.LiveView.Controller, only: [live_render: 3]

  alias SlimServe.Root
  alias SlimServe.ThumbCache
  alias SlimServe.Thumbnail
  alias SlimServeWeb.ListingLive

  # A thumbnail goes through the same gate as a download, so a hidden image has none either.
  def show(conn, %{"path" => segments, "thumb" => "1"}) do
    with {:file, file} <- resolve(segments),
         true <- Thumbnail.image?(file),
         {:ok, stat} <- File.stat(file, time: :posix) do
      thumbnail(conn, file, stat)
    else
      _ -> not_found(conn)
    end
  end

  def show(conn, %{"path" => segments}) do
    case resolve(segments) do
      {:dir, _dir} ->
        listing(conn, segments)

      # A file removed between resolve and stat is missing like any other.
      {:file, file} ->
        case File.stat(file) do
          {:ok, stat} -> download(conn, file, List.last(segments), stat.size)
          {:error, _reason} -> not_found(conn)
        end

      :error ->
        not_found(conn)
    end
  end

  # ?view=grid or ?view=list keeps the choice in a cookie and returns to the bare path; any other value is ignored.
  defp listing(conn, segments) do
    case conn.params do
      %{"view" => view} when view in ~w(grid list) ->
        conn
        |> put_resp_cookie("slimserve-view", view, max_age: 31_536_000, same_site: "Lax", http_only: true)
        |> redirect(to: ListingLive.href(segments))

      _ ->
        view = if conn.cookies["slimserve-view"] == "grid", do: "grid", else: "list"
        live_render(conn, ListingLive, session: %{"segments" => segments, "view" => view})
    end
  end

  # The path comes only from Root.resolve; the type is served with nosniff and the default-src 'self' CSP.
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
        |> send_file(206, file, first, last - first + 1)

      :unsatisfiable ->
        conn |> put_resp_header("content-range", "bytes */#{size}") |> send_resp(416, "")

      :full ->
        send_file(conn, 200, file)
    end
  end

  # One range of bytes=a-b, a- or -n; several ranges, a malformed header or an empty file get the whole file.
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

  defp resolve(segments), do: Root.resolve(Application.fetch_env!(:slimserve, :root), segments)

  # One answer for every refusal, so a 404 never tells a hidden path from a missing one.
  defp not_found(conn), do: send_resp(conn, 404, "Not Found")

  defp thumbnail(conn, file, stat) do
    max_dim = Application.fetch_env!(:slimserve, :thumb_max_dim)
    key = ThumbCache.key(file, stat, max_dim)

    # The size is checked before libvips opens the file. A file libvips cannot decode is not an image.
    with false <- stat.size > Application.fetch_env!(:slimserve, :thumb_max_file_bytes),
         :miss <- ThumbCache.lookup(key),
         # ponytail: no concurrency limit; add a bounded pool if many viewers hit a cold cache.
         {:ok, webp} <- Thumbnail.generate(file, max_dim) do
      # A cast: the cache logs a failed write, and it only costs the next request a miss.
      ThumbCache.put(key, webp)
      send_webp(conn, "miss", webp)
    else
      true -> send_resp(conn, 413, "Content Too Large")
      {:ok, webp} -> send_webp(conn, "hit", webp)
      {:error, _reason} -> not_found(conn)
    end
  end

  # x-cache says whether the bytes came from the disk cache.
  defp send_webp(conn, cache, webp) do
    conn |> put_resp_content_type("image/webp", nil) |> put_resp_header("x-cache", cache) |> send_resp(200, webp)
  end
end
