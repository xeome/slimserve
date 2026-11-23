import Config

config :logger, level: :warning

# Initialize plugs at runtime for faster test compilation
config :phoenix, :plug_init_mode, :runtime

# Sort query params output of verified routes for robust url comparisons
config :phoenix,
  sort_verified_routes_query_params: true

config :phoenix_live_view,
  enable_expensive_runtime_checks: true

config :slimserve, SlimServeWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4002],
  secret_key_base: "hfGnos88mk6PwrAwSuQjWhzaRVRYO/HGS91BUY9Ez87VjFS9rVbrA1QJBmc8d0gr",
  server: false

# Small thumbnail limits, so the tests need only small fixtures.
config :slimserve,
  thumb_max_dim: 64,
  thumb_max_file_bytes: 1024 * 1024,
  thumb_cache_bytes: 1024 * 1024,
  thumb_cache_dir: Path.join(System.tmp_dir!(), "slimserve-thumbs-test")
