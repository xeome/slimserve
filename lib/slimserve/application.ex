defmodule SlimServe.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    # config/runtime.exs sets the root everywhere but in tests, which start their own Watcher.
    root = Application.get_env(:slimserve, :root)

    children = [
      SlimServeWeb.Telemetry,
      {DNSCluster, query: Application.get_env(:slimserve, :dns_cluster_query) || :ignore},
      {Phoenix.PubSub, name: SlimServe.PubSub},
      {SlimServe.ThumbCache,
       dir: Application.fetch_env!(:slimserve, :thumb_cache_dir),
       max_bytes: Application.fetch_env!(:slimserve, :thumb_cache_bytes)},
      root && {SlimServe.Watcher, root: root},
      SlimServeWeb.Endpoint
    ]

    opts = [strategy: :one_for_one, name: SlimServe.Supervisor]
    Supervisor.start_link(Enum.reject(children, &is_nil/1), opts)
  end

  @impl true
  def config_change(changed, _new, removed) do
    SlimServeWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
