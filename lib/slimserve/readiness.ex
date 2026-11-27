defmodule SlimServe.Readiness do
  @moduledoc """
  Started after the Endpoint, so it stops
  first: on shutdown it turns not ready, then holds the Endpoint up for the accept window
  (`:shutdown_wait_ms`), so a proxy stops sending here before the listener closes.
  """
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  def child_spec(opts) do
    shutdown =
      case wait_ms() do
        :infinity -> :infinity
        ms -> ms + 1_000
      end

    %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}, shutdown: shutdown}
  end

  def ready?, do: :persistent_term.get(__MODULE__, false)

  @impl true
  def init(nil) do
    Process.flag(:trap_exit, true)
    :persistent_term.put(__MODULE__, true)
    {:ok, nil}
  end

  # A test sends :release to end the window without a clock.
  @impl true
  def terminate(_reason, nil) do
    :persistent_term.put(__MODULE__, false)
    :telemetry.execute([:slimserve, :readiness, :draining], %{}, %{})

    receive do
      :release -> :ok
    after
      wait_ms() -> :ok
    end
  end

  defp wait_ms, do: Application.get_env(:slimserve, :shutdown_wait_ms, 5_000)
end
