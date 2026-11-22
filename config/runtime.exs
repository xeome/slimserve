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
  # A default value is used in config/dev.exs and config/test.exs but you
  # want to use a different value for prod and you most likely don't want
  # to check this value into version control, so we use an environment
  # variable instead.
  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise """
      environment variable SECRET_KEY_BASE is missing.
      You can generate one by calling: mix phx.gen.secret
      """

  host = System.get_env("PHX_HOST") || "example.com"

  config :slimserve, SlimServeWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [
      ip: {0, 0, 0, 0, 0, 0, 0, 0}
    ],
    secret_key_base: secret_key_base

  config :slimserve, :dns_cluster_query, System.get_env("DNS_CLUSTER_QUERY")
end
