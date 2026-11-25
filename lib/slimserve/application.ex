defmodule SlimServe.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    # config/runtime.exs sets the root everywhere but in tests, which start their own Watcher and S3 caches.
    root = Application.get_env(:slimserve, :root)
    s3? = Application.get_env(:slimserve, :storage) == :s3

    # S3 has no watcher: its metadata cache carries changes between nodes.
    children = [
      SlimServeWeb.Telemetry,
      {DNSCluster, query: Application.get_env(:slimserve, :dns_cluster_query) || :ignore},
      {Phoenix.PubSub, name: SlimServe.PubSub},
      SlimServe.LoginLimit,
      {SlimServe.ThumbCache,
       dir: Application.fetch_env!(:slimserve, :thumb_cache_dir),
       max_bytes: Application.fetch_env!(:slimserve, :thumb_cache_bytes)},
      s3? && SlimServe.Storage.S3.Metadata,
      s3? &&
        {SlimServe.DiskCache,
         name: SlimServe.DownloadCache,
         ext: "bin",
         dir: Application.fetch_env!(:slimserve, :download_cache_dir),
         max_bytes: Application.fetch_env!(:slimserve, :download_cache_bytes)},
      s3? && Application.get_env(:slimserve, :write_password) != nil && SlimServe.Storage.S3.Capabilities,
      not s3? && root && {SlimServe.Watcher, root: root},
      SlimServeWeb.Endpoint
    ]

    opts = [strategy: :one_for_one, name: SlimServe.Supervisor]
    Supervisor.start_link(Enum.filter(children, & &1), opts)
  end

  @impl true
  def config_change(changed, _new, removed) do
    SlimServeWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
