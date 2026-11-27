defmodule SlimServe.LoginLimit do
  @moduledoc """
  Counts login attempts per client in a public ETS table: a client with 10 failures in the last
  minute is limited. An IPv6 client is keyed by its /64, which one host usually holds whole.

  Each node tells the others of every attempt it records and every login that succeeds, so the
  count spans the cluster a moment late. A limited attempt is not recorded, so a flood of them
  sends nothing.
  """
  use GenServer

  @table __MODULE__
  @max_failures 10
  @window_ms 60_000
  @topic "login_limit"

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @doc """
  Records an attempt from `ip` before its credentials are compared, so parallel requests cannot
  all pass the count. Returns `{:ok, attempt}`, for `forget/1` when the login succeeds, or
  `:limited`, when the attempt is not recorded.
  """
  def attempt(ip) do
    key = key(ip)
    attempt = {key, now(), make_ref()}
    :ets.insert(@table, attempt)

    if :ets.select_count(@table, [{{key, :"$1", :_}, [{:>, :"$1", cutoff()}], [true]}]) > @max_failures do
      :ets.delete_object(@table, attempt)
      :limited
    else
      broadcast({:login_attempt, key, elem(attempt, 2)})
      {:ok, attempt}
    end
  end

  def forget({key, _at, ref} = attempt) do
    :ets.delete_object(@table, attempt)
    broadcast({:login_forget, key, ref})
  end

  # From the server's pid, so the local server, which already holds the row, skips it.
  defp broadcast(message),
    do: :ok = Phoenix.PubSub.broadcast_from(SlimServe.PubSub, Process.whereis(__MODULE__), @topic, message)

  # An IPv4 client reaches the dual-stack listener as ::ffff:a.b.c.d, so that keeps its full address.
  defp key({0, 0, 0, 0, 0, 0xFFFF, _, _} = ip), do: ip
  defp key({a, b, c, d, _, _, _, _}), do: {a, b, c, d, 0, 0, 0, 0}
  defp key(ip), do: ip

  @impl true
  def init(nil) do
    :ets.new(@table, [:duplicate_bag, :public, :named_table, write_concurrency: true])
    :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, @topic)
    {:ok, schedule_sweep(nil)}
  end

  # Stamped with this node's clock: monotonic time differs per node.
  @impl true
  def handle_info({:login_attempt, key, ref}, state) do
    :ets.insert(@table, {key, now(), ref})
    :telemetry.execute([:slimserve, :login_limit, :peer_attempt], %{}, %{})
    {:noreply, state}
  end

  def handle_info({:login_forget, key, ref}, state) do
    :ets.match_delete(@table, {key, :_, ref})
    {:noreply, state}
  end

  def handle_info(:sweep, state) do
    :ets.select_delete(@table, [{{:_, :"$1", :_}, [{:"=<", :"$1", cutoff()}], [true]}])
    {:noreply, schedule_sweep(state)}
  end

  # A node of another version may send a message this one does not know.
  def handle_info(_unknown, state), do: {:noreply, state}

  defp schedule_sweep(state) do
    Process.send_after(self(), :sweep, @window_ms)
    state
  end

  defp now, do: System.monotonic_time(:millisecond)
  defp cutoff, do: now() - @window_ms
end
