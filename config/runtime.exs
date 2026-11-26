import Config

# Do not define any compile-time configuration in here, as it won't be applied.

if System.get_env("PHX_SERVER") do
  config :slimserve, SlimServeWeb.Endpoint, server: true
end

port = if config_env() != :test, do: String.to_integer(System.get_env("PORT", "4000"))

set = fn name -> if (value = System.get_env(name)) != "", do: value end

if config_env() != :test do
  megabytes = fn name, default -> String.to_integer(System.get_env(name, default)) * 1024 * 1024 end

  # Both set puts every route behind a login page; neither leaves the server open.
  login =
    case {set.("SLIMSERVE_USERNAME"), set.("SLIMSERVE_PASSWORD")} do
      {nil, nil} -> nil
      {user, pass} when is_binary(user) and is_binary(pass) -> {user, pass}
      _one -> raise "set both SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD, or neither"
    end

  # The secret signs the LiveView session and the CSRF token, and keys the login and write digests
  # in the cookie. Nothing else is persisted, so without login a fresh one per boot is enough: open
  # pages reload after a restart, and a write-only deployment just ends write logins on restart.
  # With login a fresh one would log every viewer out on each restart, so login needs a fixed secret.
  if config_env() == :prod and login != nil and System.get_env("SECRET_KEY_BASE") == nil do
    raise "SECRET_KEY_BASE must be set when SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD are"
  end

  write_password = set.("SLIMSERVE_WRITE_PASSWORD")

  # A viewer would hold write rights.
  if write_password != nil and match?({_user, ^write_password}, login) do
    raise "SLIMSERVE_WRITE_PASSWORD must differ from SLIMSERVE_PASSWORD"
  end

  # allow_upload raises on an extension with no known MIME type, so the boot refuses it first.
  # Lowercase, because an upload's extension is downcased before it is looked up.
  upload_types =
    case set.("SLIMSERVE_UPLOAD_TYPES") do
      nil ->
        :any

      list ->
        types =
          for ext <- String.split(list, ",", trim: true),
              ext = ext |> String.trim() |> String.trim_leading(".") |> String.downcase(),
              ext != "" do
            if not MIME.has_type?(ext), do: raise("SLIMSERVE_UPLOAD_TYPES: no MIME type is known for .#{ext}")
            "." <> ext
          end

        if types == [], do: raise("SLIMSERVE_UPLOAD_TYPES lists no extension"), else: types
    end

  storage =
    case set.("SLIMSERVE_STORAGE") do
      nil -> :local
      "local" -> :local
      "s3" -> :s3
      other -> raise "SLIMSERVE_STORAGE must be local or s3, got: #{other}"
    end

  config :slimserve, SlimServeWeb.Endpoint, http: [port: port]
  config :slimserve, login: login

  if storage == :s3 do
    config :slimserve,
      storage: :s3,
      s3: [
        bucket: set.("SLIMSERVE_S3_BUCKET") || raise("SLIMSERVE_S3_BUCKET must be set when SLIMSERVE_STORAGE=s3"),
        endpoint: set.("AWS_ENDPOINT_URL_S3"),
        region: set.("AWS_REGION") || "us-east-1",
        access_key_id: set.("AWS_ACCESS_KEY_ID") || raise("AWS_ACCESS_KEY_ID must be set when SLIMSERVE_STORAGE=s3"),
        secret_access_key:
          set.("AWS_SECRET_ACCESS_KEY") || raise("AWS_SECRET_ACCESS_KEY must be set when SLIMSERVE_STORAGE=s3"),
        token: set.("AWS_SESSION_TOKEN")
      ],
      # Objects up to 100 MB are kept here whole, so a second download does not ask the bucket.
      download_cache_bytes: megabytes.("SLIMSERVE_DOWNLOAD_CACHE_MB", "1024"),
      download_cache_dir: set.("SLIMSERVE_DOWNLOAD_CACHE_DIR") || Path.join(System.tmp_dir!(), "slimserve-downloads")
  else
    config :slimserve, root: Path.expand(System.get_env("SLIMSERVE_ROOT", "."))
  end

  config :slimserve,
    thumb_max_dim: String.to_integer(System.get_env("SLIMSERVE_THUMB_MAX_DIM", "250")),
    thumb_max_file_bytes: megabytes.("SLIMSERVE_THUMB_MAX_FILE_SIZE_MB", "10"),
    thumb_cache_bytes: megabytes.("SLIMSERVE_THUMB_CACHE_MB", "100"),
    thumb_cache_dir: System.get_env("SLIMSERVE_CACHE_DIR") || Path.join(System.tmp_dir!(), "slimserve-thumbs")

  config :slimserve,
    # A user who logs in with it can change the served files.
    write_password: write_password,
    upload_max_bytes: megabytes.("SLIMSERVE_UPLOAD_MAX_MB", "100"),
    upload_types: upload_types
end

# A prod server has no default root: serving the release's own working directory by accident would
# publish it. Only with PHX_SERVER, as the release image sets it, so a prod mix task runs without a root.
if config_env() == :prod and System.get_env("PHX_SERVER") != nil and System.get_env("SLIMSERVE_STORAGE") != "s3" and
     not File.dir?(System.get_env("SLIMSERVE_ROOT", "")) do
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

  host = set.("PHX_HOST")

  https? =
    case set.("SLIMSERVE_HTTPS") do
      nil -> false
      "true" -> true
      "false" -> false
      other -> raise "SLIMSERVE_HTTPS must be true or false, got: #{other}"
    end

  {url, ssl_redirect} =
    if https? do
      host = host || raise "PHX_HOST must be set when SLIMSERVE_HTTPS=true"

      ssl_redirect =
        Plug.SSL.init(host: host, rewrite_on: [:x_forwarded_proto], exclude: [hosts: ["localhost", "127.0.0.1"]])

      {[host: host, port: 443, scheme: "https"], ssl_redirect}
    else
      {[host: host || "localhost", port: port, scheme: "http"], nil}
    end

  config :slimserve, SlimServeWeb.Endpoint,
    url: url,
    http: [
      ip: {0, 0, 0, 0, 0, 0, 0, 0}
    ],
    secret_key_base: secret_key_base

  config :slimserve, :dns_cluster_query, System.get_env("DNS_CLUSTER_QUERY")
  config :slimserve, :ssl_redirect, ssl_redirect
end
