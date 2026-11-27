defmodule SlimServeWeb.Health do
  @moduledoc """
  Answers a proxy's probes before HTTPS, login, the session or storage.
  """
  @behaviour Plug

  import Plug.Conn

  @impl true
  def init(opts), do: opts

  @impl true
  def call(%{method: method, path_info: [".app", "healthz"]} = conn, _opts) when method in ~w(GET HEAD),
    do: answer(conn, 200)

  def call(%{method: method, path_info: [".app", "readyz"]} = conn, _opts) when method in ~w(GET HEAD),
    do: answer(conn, if(SlimServe.Readiness.ready?(), do: 200, else: 503))

  def call(conn, _opts), do: conn

  defp answer(conn, status) do
    body =
      cond do
        conn.method == "HEAD" -> ""
        status == 200 -> "ok"
        true -> "shutting down"
      end

    conn
    |> put_resp_content_type("text/plain")
    |> send_resp(status, body)
    |> halt()
  end
end
