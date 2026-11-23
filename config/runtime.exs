import Config

# Do not define any compile-time configuration in here, as it won't be applied.

# If you use `mix release`, you need to explicitly enable the server
# by passing the PHX_SERVER=true when you start it:
#
#     PHX_SERVER=true bin/slimserve start
if System.get_env("PHX_SERVER") do
  config :slimserve, SlimServeWeb.Endpoint, server: true
end

if config_env() != :test do
  config :slimserve, SlimServeWeb.Endpoint, http: [port: String.to_integer(System.get_env("PORT", "4000"))]
  # The one directory served at /. Tests set their own.
  config :slimserve, root: Path.expand(System.get_env("SLIMSERVE_ROOT", "."))
end

# A prod server has no default root: serving the release's own working directory by accident would
# publish it. Only with PHX_SERVER, as the release image sets it, so a prod mix task runs without a root.
if config_env() == :prod and System.get_env("PHX_SERVER") != nil and not File.dir?(System.get_env("SLIMSERVE_ROOT", "")) do
  raise "SLIMSERVE_ROOT must name an existing directory"
end

if config_env() == :dev do
  config :slimserve, SlimServeWeb.Endpoint,
    live_reload: [
      web_console_logger: true,
      patterns: [
        ~r"priv/static/(?!uploads/).*\.(js|css|png|jpeg|jpg|gif|svg)$"E,
        ~r"lib/slimserve_web/router\.ex$"E,
        ~r"lib/slimserve_web/(controllers|live|components)/.*\.(ex|heex)$"E
      ]
    ]
end

if config_env() == :prod do
  # Without auth the secret only signs the LiveView session and the CSRF token, and nothing
  # is persisted. So a fresh one per boot is enough: open pages reload after a restart.
  secret_key_base = System.get_env("SECRET_KEY_BASE") || Base.encode64(:crypto.strong_rand_bytes(48))

  host = System.get_env("PHX_HOST") || "example.com"

  config :slimserve, SlimServeWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [
      ip: {0, 0, 0, 0, 0, 0, 0, 0}
    ],
    secret_key_base: secret_key_base

  config :slimserve, :dns_cluster_query, System.get_env("DNS_CLUSTER_QUERY")
end
