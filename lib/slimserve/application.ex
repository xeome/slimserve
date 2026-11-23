defmodule SlimServe.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      SlimServeWeb.Telemetry,
      {DNSCluster, query: Application.get_env(:slimserve, :dns_cluster_query) || :ignore},
      {Phoenix.PubSub, name: SlimServe.PubSub},
      {SlimServe.ThumbCache,
       dir: Application.fetch_env!(:slimserve, :thumb_cache_dir),
       max_bytes: Application.fetch_env!(:slimserve, :thumb_cache_bytes)},
      SlimServeWeb.Endpoint
    ]

    opts = [strategy: :one_for_one, name: SlimServe.Supervisor]
    Supervisor.start_link(children, opts)
  end

  @impl true
  def config_change(changed, _new, removed) do
    SlimServeWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
