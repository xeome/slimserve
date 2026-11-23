defmodule SlimServeWeb.BrowseController do
  use SlimServeWeb, :controller

  import Phoenix.LiveView.Controller, only: [live_render: 3]

  alias SlimServe.Root
  alias SlimServe.ThumbCache
  alias SlimServe.Thumbnail

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

  # The path comes only from Root.resolve; the type is served with nosniff and the default-src 'self' CSP.
  # sobelow_skip ["Traversal.SendFile", "XSS.ContentType"]
  def show(conn, %{"path" => segments}) do
    case resolve(segments) do
      {:dir, _dir} ->
        live_render(conn, SlimServeWeb.ListingLive, session: %{"segments" => segments})

      # The type comes from the requested name: a link named x.txt is text, whatever it points to.
      {:file, file} ->
        conn |> put_resp_content_type(MIME.from_path(List.last(segments)), nil) |> send_file(200, file)

      :error ->
        not_found(conn)
    end
  end

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
