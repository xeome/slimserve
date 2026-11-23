defmodule SlimServeWeb.Router do
  use SlimServeWeb, :router

  # The usual :browser pipeline without `plug :accepts`, so a download answers any Accept header.
  pipeline :serve do
    plug :fetch_session
    # data: images: the heroicons masks and the graphite grain are inline SVG data URLs.
    # Before the login gate, so its refusals carry the headers too.
    plug :put_secure_browser_headers, %{
      "content-security-policy" => "default-src 'self'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'self'"
    }

    # Before CSRF, so with login off a POST to the login paths gets the 404 of an unserved path.
    plug SlimServeWeb.Login
    plug :fetch_live_flash
    plug :put_root_layout, html: {SlimServeWeb.Layouts, :root}
    plug :protect_from_forgery
  end

  pipeline :api do
    plug :accepts, ["json"]
  end

  if Application.compile_env(:slimserve, :dev_routes) do
    # If you want to use the LiveDashboard in production, you should put
    # it behind authentication and allow only admins to access it.
    # If your application does not have an admins-only section yet,
    # you can use Plug.BasicAuth to set up some basic authentication
    # as long as you are also using SSL (which you should anyway).
    import Phoenix.LiveDashboard.Router

    pipeline :dashboard_csp do
      plug :put_dashboard_csp_nonce
    end

    scope "/dev" do
      pipe_through [:serve, :dashboard_csp]

      # The app's socket lives under /.app (see the endpoint), not at the dashboard's default /live.
      live_dashboard "/dashboard",
        metrics: SlimServeWeb.Telemetry,
        csp_nonce_assign_key: :csp_nonce,
        live_socket_path: "/.app/live"
    end

    # LiveDashboard renders inline <script> and <style> tags; a per-request nonce
    # admits them on this scope only, so app pages keep the stricter policy.
    defp put_dashboard_csp_nonce(conn, _opts) do
      nonce = 18 |> :crypto.strong_rand_bytes() |> Base.url_encode64(padding: false)
      [csp] = get_resp_header(conn, "content-security-policy")
      sources = "'self' 'nonce-#{nonce}'"

      conn
      |> assign(:csp_nonce, nonce)
      |> put_resp_header("content-security-policy", "#{csp}; script-src #{sources}; style-src #{sources}")
    end
  end

  # Last, because it matches every path: a route declared after it is unreachable.
  scope "/", SlimServeWeb do
    pipe_through :serve

    # Under /.app, where no served file can collide.
    get "/.app/login", LoginController, :new
    post "/.app/login", LoginController, :create
    post "/.app/logout", LoginController, :delete

    get "/*path", BrowseController, :show
  end
end
