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
  megabytes = fn name, default -> String.to_integer(System.get_env(name, default)) * 1024 * 1024 end

  # An empty variable counts as unset.
  set = fn name -> if System.get_env(name) != "", do: System.get_env(name) end

  # Both set puts every route behind a login page; neither leaves the server open. Tests set their own.
  login =
    case {set.("SLIMSERVE_USERNAME"), set.("SLIMSERVE_PASSWORD")} do
      {nil, nil} -> nil
      {user, pass} when is_binary(user) and is_binary(pass) -> {user, pass}
      _one -> raise "set both SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD, or neither"
    end

  # Without login the secret only signs the LiveView session and the CSRF token, and nothing
  # is persisted. So a fresh one per boot is enough: open pages reload after a restart.
  # With login it also keys the login digest in the cookie, and a fresh one per boot would log
  # everyone out on each restart, so login needs a fixed secret.
  if config_env() == :prod and login != nil and System.get_env("SECRET_KEY_BASE") == nil do
    raise "SECRET_KEY_BASE must be set when SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD are"
  end

  config :slimserve, SlimServeWeb.Endpoint, http: [port: String.to_integer(System.get_env("PORT", "4000"))]
  config :slimserve, login: login

  config :slimserve,
    # The one directory served at /. Tests set their own.
    root: Path.expand(System.get_env("SLIMSERVE_ROOT", ".")),
    # Thumbnails: the longest side in pixels, the largest source file, and the disk cache. Tests set their own.
    thumb_max_dim: String.to_integer(System.get_env("SLIMSERVE_THUMB_MAX_DIM", "250")),
    thumb_max_file_bytes: megabytes.("SLIMSERVE_THUMB_MAX_FILE_SIZE_MB", "10"),
    thumb_cache_bytes: megabytes.("SLIMSERVE_THUMB_CACHE_MB", "100"),
    thumb_cache_dir: System.get_env("SLIMSERVE_CACHE_DIR") || Path.join(System.tmp_dir!(), "slimserve-thumbs")
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
