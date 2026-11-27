defmodule SlimServe.Cluster do
  @moduledoc """
  Starts a peer node that runs the app beside the test node, and forwards its telemetry to a test.
  """
  import ExUnit.Assertions
  import ExUnit.Callbacks

  @doc """
  Under the test supervisor, not linked to the test, so its exit does not take the test down; the
  test's end stops it.
  """
  def start_peer(env \\ []) do
    if not Node.alive?() do
      # Node.start fails with no epmd, and CI starts none. A second daemon finds the port taken and exits.
      System.cmd("epmd", ["-daemon"])
      {:ok, _pid} = Node.start(:"slimserve-a@127.0.0.1", name_domain: :longnames)
    end

    args = [~c"-setcookie", Atom.to_charlist(Node.get_cookie()) | Enum.flat_map(:code.get_path(), &[~c"-pa", &1])]
    name = :peer.random_name()
    opts = %{name: name, host: ~c"127.0.0.1", longnames: true, args: args}
    start_supervised!(%{id: make_ref(), start: {:peer, :start_link, [opts]}, restart: :temporary})
    node = :"#{name}@127.0.0.1"

    for {app, _description, _version} <- Application.loaded_applications() do
      :erpc.call(node, Application, :load, [app])
      :ok = :erpc.call(node, Application, :put_all_env, [[{app, Application.get_all_env(app)}]])
    end

    :ok = :erpc.call(node, Application, :put_all_env, [[slimserve: env]])
    {:ok, _apps} = :erpc.call(node, Application, :ensure_all_started, [:slimserve])
    {:ok, _apps} = :erpc.call(node, Application, :ensure_all_started, [:ex_unit])
    await_pubsub(node)
    node
  end

  @doc """
  A peer's handlers go with the peer.
  """
  def watch(node, event) do
    id = {__MODULE__, make_ref()}
    :ok = :erpc.call(node, :telemetry, :attach, [id, event, &__MODULE__.forward/4, self()])
    if node == node(), do: on_exit(fn -> :telemetry.detach(id) end)
  end

  # A named function: an anonymous one from a test module is undefined on the peer.
  @doc false
  def forward(event, _measurements, metadata, pid), do: send(pid, {:telemetry, node(), event, metadata})

  defp await_pubsub(b) do
    {ref, groups} = :pg.monitor_scope(Phoenix.PubSub)
    if not Enum.any?(List.flatten(Map.values(groups)), &(node(&1) == b)), do: await_join(ref, b)
    :pg.demonitor(Phoenix.PubSub, ref)
  end

  defp await_join(ref, b) do
    assert_receive {^ref, :join, _group, pids}
    if not Enum.any?(pids, &(node(&1) == b)), do: await_join(ref, b)
  end
end
