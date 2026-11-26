defmodule SlimServe.Storage.S3.Metadata do
  @moduledoc """
  SlimServe is the bucket's only writer, so an entry lives until a write changes its path; the
  expiry is a backstop for a change a node never heard. A node wakes its own open pages only after
  the burst's refill worker has exited, so a woken page reads the refilled entry or a miss, never an
  entry the change made stale.

  A fill that read the store before a clear must not land after it. The clear bumps a generation
  first and deletes second; a fill inserts first and checks the generation second, and takes its
  entry back when the generation moved. Either the clear's delete or the fill's check catches it.
  """
  use GenServer

  alias SlimServe.Root
  alias SlimServe.Storage.S3

  @table __MODULE__
  @ttl_ms 600_000
  @sweep_ms 60_000
  # As the page's own re-list, so a multi-file upload fills its folder once.
  @burst_ms 100
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
  The paths a write `added` and `removed` (an implicit folder above them can appear or vanish), and
  `trees`, folders moved or deleted with everything below them.

  The default timeout holds: Metadata never waits on the store, so a call waits only behind clears.
  """
  @spec changed([[String.t()]], [[String.t()]], [[String.t()]]) :: :ok
  def changed(added, removed, trees), do: GenServer.call(__MODULE__, {:changed, added, removed, trees})

  @impl true
  def init(nil) do
    # A fill runs in a linked task and parses what the provider sent, so its crash is a message here.
    Process.flag(:trap_exit, true)
    :ets.new(@table, [:named_table, :public, read_concurrency: true])
    :ets.insert(@table, {:generation, 0})
    :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, @topic)
    Process.send_after(self(), :sweep, @sweep_ms)
    {:ok, %{burst: nil, filling: %{}}}
  end

  @impl true
  def handle_call({:changed, _added, _removed, _trees} = change, _from, state) do
    :ok = Phoenix.PubSub.broadcast_from(SlimServe.PubSub, self(), @topic, change)
    {:reply, :ok, clear(change, state)}
  end

  @impl true
  def handle_info({:changed, _added, _removed, _trees} = change, state), do: {:noreply, clear(change, state)}

  def handle_info(:fill, %{burst: {keys, wake}} = state) do
    {:ok, pid} = Task.start_link(fn -> keys |> Task.async_stream(&S3.refill/1, timeout: :infinity) |> Stream.run() end)
    {:noreply, %{state | burst: nil, filling: Map.put(state.filling, pid, wake)}}
  end

  def handle_info({:EXIT, pid, _reason}, %{filling: filling} = state) when is_map_key(filling, pid) do
    {wake, filling} = Map.pop!(filling, pid)
    for {topic, message} <- Enum.uniq(wake), do: Phoenix.PubSub.local_broadcast(SlimServe.PubSub, topic, message)
    {:noreply, %{state | filling: filling}}
  end

  def handle_info(:sweep, state) do
    now = System.monotonic_time(:millisecond)
    :ets.select_delete(@table, [{{:_, :_, :"$1"}, [{:<, :"$1", now}], [true]}])
    Process.send_after(self(), :sweep, @sweep_ms)
    {:noreply, state}
  end

  defp clear({:changed, added, removed, trees}, state) do
    :ets.update_counter(@table, :generation, 1)
    paths = added ++ removed
    names = Enum.map(paths, &Enum.join(&1, "/"))

    # shortcut: scans every entry per change; past ~100k entries, select each kind's path range in an ordered_set.
    under = for key <- :ets.select(@table, [{{:"$1", :_, :_}, [], [:"$1"]}]), under_any?(key, names), do: key
    keys = Enum.uniq(under ++ Enum.flat_map(added, &appeared/1) ++ Enum.flat_map(removed, &vanished/1))
    cleared = for key <- keys, :ets.take(@table, key) != [], do: key

    fill = for key <- cleared, elem(key, 0) in [:list, :folder], not under_any?(key, names), do: key

    dirs = for path <- paths, n <- 0..(length(path) - 1)//1, uniq: true, do: path |> Enum.take(n) |> Enum.join("/")

    wake =
      for(dir <- dirs, do: {Root.topic(dir), {:dir_changed, dir}}) ++
        for tree <- trees, do: {tree_topic(), {:tree_changed, Enum.join(tree, "/")}}

    {keys, waking} = state.burst || burst()
    %{state | burst: {MapSet.union(keys, MapSet.new(fill)), waking ++ wake}}
  end

  defp burst do
    Process.send_after(self(), :fill, @burst_ms)
    {MapSet.new(), []}
  end

  defp under_any?({_kind, name}, paths), do: Enum.any?(paths, &(name == &1 or String.starts_with?(name, &1 <> "/")))

  # An ancestor the cache holds as absent may appear with it, even one
  # proven present before the write, when a concurrent delete emptied it first.
  defp appeared(path) do
    [
      {:list, prefix(Enum.drop(path, -1))}
      | for(n <- 1..(length(path) - 1)//1, key <- absent(Enum.take(path, n)), do: key)
    ]
  end

  defp absent(folder) do
    name = Enum.join(folder, "/")

    for key <- [{:folder, name}, {:list, prefix(Enum.drop(folder, -1))}],
        [{_key, value, _expires}] <- [:ets.lookup(@table, key)],
        absent?(value, name),
        do: key
  end

  defp absent?({:ok, false}, _name), do: true
  defp absent?({:ok, {_objects, prefixes}}, name), do: (name <> "/") not in prefixes
  defp absent?(_present, _name), do: false

  defp vanished(path) do
    for n <- 0..(length(path) - 1)//1,
        folder = Enum.take(path, n),
        key <- [{:list, prefix(folder)}, {:folder, Enum.join(folder, "/")}],
        do: key
  end

  defp prefix([]), do: ""
  defp prefix(folder), do: Enum.join(folder, "/") <> "/"
end
