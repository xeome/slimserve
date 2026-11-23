defmodule SlimServeWeb.BrowseController do
  use SlimServeWeb, :controller

  import Phoenix.LiveView.Controller, only: [live_render: 3]

  alias SlimServe.Root

  # The path comes only from Root.resolve; the type is served with nosniff and the default-src 'self' CSP.
  # sobelow_skip ["Traversal.SendFile", "XSS.ContentType"]
  def show(conn, %{"path" => segments}) do
    case Root.resolve(Application.fetch_env!(:slimserve, :root), segments) do
      {:dir, _dir} ->
        live_render(conn, SlimServeWeb.ListingLive, session: %{"segments" => segments})

      # The type comes from the requested name: a link named x.txt is text, whatever it points to.
      {:file, file} ->
        conn |> put_resp_content_type(MIME.from_path(List.last(segments)), nil) |> send_file(200, file)

      # One answer for every refusal, so a 404 never tells a hidden path from a missing one.
      :error ->
        send_resp(conn, 404, "Not Found")
    end
  end
end
