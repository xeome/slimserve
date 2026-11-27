defmodule SlimServeWeb.Endpoint do
  use Phoenix.Endpoint, otp_app: :slimserve

  @session_options [
    store: :cookie,
    key: "_slimserve_key",
    signing_salt: "UB11q4eI",
    same_site: "Lax"
  ]
  @http_session Plug.Session.init(Keyword.put(@session_options, :secure, false))
  @https_session Plug.Session.init(Keyword.put(@session_options, :secure, true))

  # A probe is answered over plain HTTP too. Rewrite or redirect before Phoenix dispatches either socket transport.
  @impl Plug
  def call(conn, opts) do
    conn = SlimServeWeb.Health.call(conn, [])
    ssl_redirect = Application.get_env(:slimserve, :ssl_redirect)
    conn = if ssl_redirect != nil and not conn.halted, do: Plug.SSL.call(conn, ssl_redirect), else: conn

    if conn.halted, do: conn, else: super(conn, opts)
  end

  # Everything of the app's own lives under /.app: a dot path is never a served file,
  # so no file in the root can collide with the socket or an asset.
  socket "/.app/live", Phoenix.LiveView.Socket,
    websocket: [connect_info: [session: @session_options]],
    longpoll: [connect_info: [session: @session_options]]

  plug Plug.Static,
    at: "/.app",
    from: :slimserve,
    gzip: not code_reloading?,
    only: SlimServeWeb.static_paths(),
    raise_on_missing_only: code_reloading?

  if code_reloading? do
    socket "/phoenix/live_reload/socket", Phoenix.LiveReloader.Socket
    plug Phoenix.LiveReloader
    plug Phoenix.CodeReloader
  end

  plug Phoenix.LiveDashboard.RequestLogger,
    param_key: "request_logger",
    cookie_key: "request_logger"

  plug Plug.RequestId
  plug Plug.Telemetry, event_prefix: [:phoenix, :endpoint]

  plug Plug.Parsers,
    parsers: [:urlencoded, :multipart, :json],
    pass: ["*/*"],
    json_decoder: Phoenix.json_library()

  plug Plug.MethodOverride
  # Plug.Head turns HEAD into GET, so controllers read this to skip an S3 fetch or a thumbnail render.
  plug :mark_head
  plug Plug.Head
  plug SlimServeWeb.ClientIp
  plug :session
  plug SlimServeWeb.Router

  defp mark_head(conn, _opts), do: put_private(conn, :head?, conn.method == "HEAD")

  defp session(conn, _opts) do
    session = if Application.get_env(:slimserve, :ssl_redirect), do: @https_session, else: @http_session
    Plug.Session.call(conn, session)
  end
end
