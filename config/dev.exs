import Config

config :logger, :default_formatter, format: "[$level] $message\n"

# Initialize plugs at runtime for faster development compilation
config :phoenix, :plug_init_mode, :runtime

# Set a higher stacktrace during development. Avoid configuring such
# in production as building large stacktraces may be expensive.
config :phoenix, :stacktrace_depth, 20

config :phoenix_live_view,
  # Changing this configuration will require mix clean and a full recompile.
  debug_heex_annotations: true,
  debug_attributes: true,
  enable_expensive_runtime_checks: true

config :slimserve, SlimServeWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}],
  check_origin: false,
  code_reloader: true,
  debug_errors: true,
  secret_key_base: "1OwGDELy3/Fb8VoIFThFgAQePTVAM6wRPFSmsRAi/um2Xn5xwouj8OIgPnBakaC4",
  watchers: [
    esbuild: {Esbuild, :install_and_run, [:slimserve, ~w(--sourcemap=inline --watch)]},
    tailwind: {Tailwind, :install_and_run, [:slimserve, ~w(--watch)]}
  ]
