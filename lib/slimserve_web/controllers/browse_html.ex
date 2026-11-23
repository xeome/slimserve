defmodule SlimServeWeb.BrowseHTML do
  @moduledoc false
  use SlimServeWeb, :html

  import Phoenix.Controller, only: [put_root_layout: 2, put_view: 2, render: 3]
  import Plug.Conn

  alias SlimServeWeb.Login

  embed_templates "browse_html/*"

  def accepts_html?(conn), do: conn |> get_req_header("accept") |> Enum.any?(&String.contains?(&1, "text/html"))

  # One answer for every refusal, so a 404 never tells a hidden path from a missing one.
  def send_not_found(conn) do
    if accepts_html?(conn) do
      conn
      |> put_status(:not_found)
      |> put_root_layout(html: {Layouts, :root})
      |> put_view(html: __MODULE__)
      |> render("not_found.html",
        layout: false,
        flash: conn.assigns[:flash] || %{},
        page_title: "Page not found",
        write?: Login.write?(get_session(conn))
      )
      |> halt()
    else
      conn |> send_resp(404, "Not Found") |> halt()
    end
  end
end
