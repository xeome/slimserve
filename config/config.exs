import Config

config :esbuild,
  version: "0.25.4",
  slimserve: [
    args:
      ~w(js/app.js --bundle --target=es2022 --outdir=../priv/static/assets/js --external:/fonts/* --external:/images/* --alias:@=.),
    cd: Path.expand("../assets", __DIR__),
    env: %{"NODE_PATH" => [Path.expand("../deps", __DIR__), Mix.Project.build_path()]}
  ]

config :logger, :default_formatter,
  format: "$time $metadata[$level] $message\n",
  metadata: [:request_id]

config :phoenix, :json_library, Jason

config :phoenix_live_view,
  # Used for Phoenix.LiveView.ColocatedCSS.
  root_tag_attribute: "phx-r"

config :slimserve, SlimServeWeb.Endpoint,
  url: [host: "localhost"],
  # Matches `Plug.Static` in the endpoint.
  static_url: [path: "/.app"],
  # The host a self-hosted server is reached by is not known ahead: accept the request's own.
  check_origin: :conn,
  adapter: Bandit.PhoenixAdapter,
  render_errors: [
    formats: [html: SlimServeWeb.ErrorHTML, json: SlimServeWeb.ErrorJSON],
    layout: false
  ],
  pubsub_server: SlimServe.PubSub,
  live_view: [signing_salt: "5gwYvl4A"]

config :slimserve,
  namespace: SlimServe,
  generators: [timestamp_type: :utc_datetime]

config :tailwind,
  version: "4.3.3",
  slimserve: [
    args: ~w(
      --input=assets/css/app.css
      --output=priv/static/assets/css/app.css
    ),
    cd: Path.expand("..", __DIR__),
    env: %{"NODE_PATH" => [Path.expand("../deps", __DIR__), Mix.Project.build_path()]}
  ]

# This must remain at the bottom
# of this file so it overrides the configuration defined above.
import_config "#{config_env()}.exs"
