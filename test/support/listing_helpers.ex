defmodule SlimServeWeb.ListingHelpers do
  @moduledoc """
  Waits on a listing page's `[:slimserve, :listing, :relisted]` event, which it sends at the end of
  each relist. Each wait attaches its handler before it checks or acts, so no relist slips past it,
  and gives up only after the assert_receive timeout passes with no event: a hang, not a slow build.
  """
  import ExUnit.Assertions

  alias SlimServe.Root

  @event [:slimserve, :listing, :relisted]

  def await_page(view, check) do
    case watching(view, &relists(view, check, &1, 0)) do
      {:ok, value} -> value
      {:hung, seen} -> flunk("after #{seen} relists the page still showed #{summary(view)}")
    end
  end

  @doc false
  def relisted(_event, _measurements, _metadata, {pid, test, ref}) do
    if self() == pid, do: send(test, {ref, :relisted})
  end

  # The page may run on another node.
  defp watching(view, fun) do
    ref = make_ref()
    node = node(view.pid)
    :ok = :erpc.call(node, :telemetry, :attach, [ref, @event, &__MODULE__.relisted/4, {view.pid, self(), ref}])

    try do
      fun.(ref)
    after
      :erpc.call(node, :telemetry, :detach, [ref])
    end
  end

  defp relists(view, check, ref, seen) do
    if value = check.(view) do
      {:ok, value}
    else
      receive do
        {^ref, :relisted} -> relists(view, check, ref, seen + 1)
      after
        timeout() -> {:hung, seen}
      end
    end
  end

  defp timeout, do: Application.fetch_env!(:ex_unit, :assert_receive_timeout)

  defp summary(view) do
    doc = view |> Phoenix.LiveViewTest.render() |> LazyHTML.from_fragment()

    inspect(%{
      entries: doc |> LazyHTML.query("#entries tr[data-name]") |> LazyHTML.attribute("data-name"),
      tree: doc |> LazyHTML.query("#tree .node") |> LazyHTML.attribute("data-path")
    })
  catch
    :exit, reason -> "no page, because the view exited: #{inspect(reason)}"
  end

  @doc "inotifywait reports nothing until its watches are set."
  def ready(root) do
    :ok = Phoenix.PubSub.subscribe(SlimServe.PubSub, Root.topic(""))
    # One write broadcasts more than once, so an earlier call can leave an event that a stopped watcher sent.
    flush()
    poke(root, 50)
    :ok = Phoenix.PubSub.unsubscribe(SlimServe.PubSub, Root.topic(""))
    flush()
  end

  defp flush do
    receive do
      {:dir_changed, _dir} -> flush()
    after
      0 -> :ok
    end
  end

  defp poke(root, tries) do
    File.write!(Path.join(root, ".ready"), "x")

    receive do
      {:dir_changed, _dir} -> :ok
    after
      100 -> if tries > 0, do: poke(root, tries - 1), else: flunk("the watcher never reported an event")
    end
  end
end
