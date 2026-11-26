defmodule SlimServeWeb.ListingLive.Tree do
  @moduledoc """
  The sidebar's folder tree and the Move to… picker.
  """
  use SlimServeWeb, :html

  alias SlimServe.Storage
  alias SlimServeWeb.ListingLive

  @type folder :: %{name: String.t(), count: non_neg_integer() | nil, dirs?: boolean()}
  @type t :: %{[String.t()] => [folder()]}

  @spec put(t(), [String.t()], [String.t()], [[String.t()]]) :: t()
  def put(tree, path, names, changed) do
    known = for f <- Map.get(tree, path, []), (path ++ [f.name]) not in changed, into: %{}, do: {f.name, f}

    level =
      names
      |> Task.async_stream(&(known[&1] || Map.put(count(path ++ [&1]), :name, &1)), timeout: :infinity)
      |> Enum.map(fn {:ok, folder} -> folder end)

    Map.put(tree, path, level)
  end

  @spec dirs([Storage.entry()]) :: [String.t()]
  def dirs(entries), do: for(%{dir?: true, name: name} <- entries, do: name)

  @spec expand(t(), [String.t()]) :: t()
  def expand(tree, path) do
    case Storage.list(path) do
      {:ok, listed} -> put(tree, path, dirs(listed), [])
      _ -> tree
    end
  end

  defp count(path) do
    case Storage.list(path) do
      {:ok, listed} -> %{count: length(listed), dirs?: Enum.any?(listed, & &1.dir?)}
      _ -> %{count: nil, dirs?: false}
    end
  end

  attr :tree, :map, required: true
  attr :path, :list, default: []
  attr :current, :list, required: true, doc: "the open folder"
  attr :pick, :any, default: false, doc: "false in the sidebar; the picked folder's path (or nil) in the picker"
  attr :drop?, :boolean, default: false, doc: "folders take a dragged row"

  def nodes(assigns) do
    assigns =
      assign(assigns, :folders, for(f <- Map.get(assigns.tree, assigns.path, []), do: {f, assigns.path ++ [f.name]}))

    ~H"""
    <ul class="tree">
      <%!-- A fold is a sticky JS class on this li, which a patch keeps. The id exists only while the folder is
           expanded, so a relist keeps the fold on its folder, and collapsing the folder replaces the element and drops it. --%>
      <li
        :for={{f, path} <- @folders}
        id={
          @pick == false && Map.has_key?(@tree, path) &&
            "tree-" <> Base.url_encode64(Enum.join(path, "/"), padding: false)
        }
      >
        <div
          class={["node", @pick != false && @pick == path && "on"]}
          data-path={Enum.join(path, "/")}
          data-drop={@drop? && path != @current && Enum.join(path, "/")}
          aria-expanded={Map.has_key?(@tree, path) && "true"}
        >
          <%= if @pick == false do %>
            <button
              :if={Map.has_key?(@tree, path) or f.dirs?}
              type="button"
              class="chev"
              aria-expanded={to_string(Map.has_key?(@tree, path))}
              aria-label={"Subfolders of " <> f.name}
              phx-click={
                if(Map.has_key?(@tree, path),
                  do:
                    JS.toggle_class("folded", to: {:closest, "li"})
                    |> JS.toggle_attribute({"aria-expanded", "false", "true"}),
                  else: JS.push("tree_expand", value: %{path: ListingLive.href(path)})
                )
              }
            >
              <.icon name="hero-chevron-down" class="size-3" />
            </button>
            <span :if={!Map.has_key?(@tree, path) and !f.dirs?} class="chev"></span>
            <a href={ListingLive.href(path)} data-open aria-current={path == @current && "page"}>
              <.icon
                name={if(Map.has_key?(@tree, path), do: "hero-folder-open", else: "hero-folder")}
                class="size-4 fi"
              />
              <span class="nm">{f.name}</span>
              <span class="ct">{f.count}</span>
            </a>
          <% else %>
            <span :if={!Map.has_key?(@tree, path) and !f.dirs?} class="chev"></span>
            <button
              :if={Map.has_key?(@tree, path) or f.dirs?}
              type="button"
              class="chev"
              phx-click="move_expand"
              phx-value-path={Enum.join(path, "/")}
              aria-label={if(Map.has_key?(@tree, path), do: "Collapse ", else: "Expand ") <> f.name}
            >
              <.icon
                name={
                  if(Map.has_key?(@tree, path), do: "hero-chevron-down", else: "hero-chevron-right")
                }
                class="size-3"
              />
            </button>
            <button
              type="button"
              class="pick"
              phx-click="move_pick"
              phx-value-path={Enum.join(path, "/")}
              aria-pressed={to_string(@pick == path)}
            >
              <.icon name="hero-folder" class="size-4 fi" />
              <span class="nm">{f.name}</span>
              <span class="ct">{if(path == @current, do: "current", else: f.count)}</span>
            </button>
          <% end %>
        </div>
        <.nodes
          :if={Map.has_key?(@tree, path)}
          tree={@tree}
          path={path}
          current={@current}
          pick={@pick}
          drop?={@drop?}
        />
      </li>
    </ul>
    """
  end
end
