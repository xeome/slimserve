defmodule SlimServe.Storage.S3.Metadata do
  @moduledoc """
  A change clears the whole table on every node: writes are rare next to reads, and a cleared cache
  can only be too fresh. Each node's owner clears its own table before it wakes its own open pages,
  so a page woken by a change never re-lists from the cache the change made stale.

  A fill that read the store before a clear must not land after it. The clear bumps a generation
  first and deletes second; a fill inserts first and checks the generation second, and takes its
  entry back when the generation moved. Either the clear's delete or the fill's check catches it.
  """
  use GenServer

  alias SlimServe.Root

  @table __MODULE__
  @ttl_ms 10_000
  @sweep_ms 60_000
  @topic "s3:changes"

  @doc "The topic every open S3 page subscribes to beside its folder's, for `{:tree_changed, rel}`."
  def tree_topic, do: "s3:tree"

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @doc """
  The cached value under `key`, or `fun`'s, which is cached unless it is `{:error, _}`: an outage is
  asked again.
  """
  @spec fetch(term(), boolean(), (-> value)) :: value when value: term()
  def fetch(key, fresh?, fun) do
    now = System.monotonic_time(:millisecond)

    case not fresh? and :ets.lookup(@table, key) do
      [{^key, value, expires}] when expires > now -> value
      _ -> fill(key, fun)
    end
  end

  defp fill(key, fun) do
    [{:generation, generation}] = :ets.lookup(@table, :generation)

    case fun.() do
      {:error, _reason} = error ->
        error

      value ->
        entry = {key, value, System.monotonic_time(:millisecond) + @ttl_ms}
        :ets.insert(@table, entry)
        if :ets.lookup(@table, :generation) != [{:generation, generation}], do: :ets.delete_object(@table, entry)
        value
    end
  end

  @doc """
  `paths` whose folder and every folder above it may list differently
  (an implicit folder can appear or vanish), and `trees`, folders moved or deleted with everything
  below them. Clears this node's cache before it returns, then every other node's.
  """
  @spec changed([[String.t()]], [[String.t()]]) :: :ok
  def changed(paths, trees \\ []), do: GenServer.call(__MODULE__, {:changed, paths, trees})

  @impl true
  def init(nil) do
    :ets.new(@table, [:named_table, :public, read_concurrency: true])
    :ets.insert(@table, {:generation, 0})
    :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, @topic)
    Process.send_after(self(), :sweep, @sweep_ms)
    {:ok, nil}
  end

  @impl true
  def handle_call({:changed, paths, trees}, _from, state) do
    :ok = Phoenix.PubSub.broadcast_from(SlimServe.PubSub, self(), @topic, {:changed, paths, trees})
    apply_change(paths, trees)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info({:changed, paths, trees}, state) do
    apply_change(paths, trees)
    {:noreply, state}
  end

  def handle_info(:sweep, state) do
    now = System.monotonic_time(:millisecond)
    :ets.select_delete(@table, [{{:_, :_, :"$1"}, [{:<, :"$1", now}], [true]}])
    Process.send_after(self(), :sweep, @sweep_ms)
    {:noreply, state}
  end

  defp apply_change(paths, trees) do
    :ets.update_counter(@table, :generation, 1)
    # Only 3-tuple entries: the {:generation, n} row stays.
    :ets.select_delete(@table, [{{:_, :_, :_}, [], [true]}])

    dirs = for path <- paths, n <- 0..(length(path) - 1)//1, uniq: true, do: path |> Enum.take(n) |> Enum.join("/")
    for dir <- dirs, do: Phoenix.PubSub.local_broadcast(SlimServe.PubSub, Root.topic(dir), {:dir_changed, dir})

    for tree <- trees,
        do: Phoenix.PubSub.local_broadcast(SlimServe.PubSub, tree_topic(), {:tree_changed, Enum.join(tree, "/")})
  end
end
