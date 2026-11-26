defmodule SlimServeWeb.Layouts do
  @moduledoc false
  use SlimServeWeb, :html

  alias SlimServeWeb.Login

  embed_templates "layouts/*"

  attr :flash, :map, required: true, doc: "the map of flash messages"

  attr :current_scope, :map,
    default: nil,
    doc: "the current [scope](https://phoenix.hexdocs.pm/scopes.html)"

  attr :login_page, :boolean, default: false, doc: "hides the logout button"
  attr :write?, :boolean, default: false, doc: "the session holds write rights"
  attr :rail, :string, default: nil, doc: ~s(the sidebar's stored state: "rail", "open", or nil for none yet)

  slot :inner_block, required: true
  slot :sidebar, doc: "the sidebar's content; without it the page keeps the plain top bar"

  # Every page behind the login gate is logged in by construction, so the button shows whenever login is on.
  # With only a write password, viewing needs no login, so the header offers one for editing instead.
  def app(assigns) do
    assigns =
      assign(assigns,
        logout?: (Login.on?() or assigns.write?) and not assigns.login_page,
        edit_login?: Login.write_on?() and not Login.on?() and not assigns.write? and not assigns.login_page,
        user:
          case Application.get_env(:slimserve, :login) do
            {user, _pass} -> user
            _ -> nil
          end
      )

    ~H"""
    <div :if={@sidebar != []} id="app-shell" class="shell">
      <%!-- CSS folds it at phone width until the hook stores one. --%>
      <nav
        id="side"
        class={["side", @rail == "rail" && "rail", is_nil(@rail) && "rail-auto"]}
        aria-label="Folders"
        phx-hook=".Rail"
      >
        <div class="top">
          <button
            type="button"
            id="rail-toggle"
            class="item tip"
            data-tip="Toggle sidebar   ["
            aria-label="Toggle sidebar"
            aria-controls="side"
            phx-click={JS.toggle_class("rail", to: "#side")}
          >
            <.icon name="hero-view-columns" class="size-4" />
          </button>
          <a href={~p"/"} class="brand fades">
            <span class="mark"><.icon name="hero-folder-open" class="size-3" /></span> SlimServe
          </a>
        </div>
        {render_slot(@sidebar)}
        <div class="who fades">
          <span class="session-label">
            {cond do
              @write? and @user -> "Editing · "
              @write? -> "Editing"
              @logout? -> "Viewer"
              true -> "Guest"
            end}<b :if={@write? and @user}>{@user}</b>
          </span>
          <a :if={@edit_login?} href={~p"/.app/login"} id="edit-login" class="btn">Log in to edit</a>
          <.form :if={@logout?} for={to_form(%{})} action={~p"/.app/logout"} id="logout-form">
            <button type="submit" class="btn">Log out</button>
          </.form>
        </div>
      </nav>
      <script :type={Phoenix.LiveView.ColocatedHook} name=".Rail">
        // The state is per device: a cookie with path=/, so every folder reads the same one, and the
        // server renders the first paint from it.
        const phone = () => window.matchMedia("(max-width: 820px)").matches
        const store = folded => {
          document.cookie = `slimserve-rail=${folded ? "rail" : "open"}; path=/; max-age=31536000; samesite=lax`
        }
        export default {
          mounted() {
            const side = this.el
            const toggle = side.querySelector("#rail-toggle")
            if (side.classList.contains("rail-auto")) {
              if (phone()) this.js().addClass(side, "rail")
              this.js().removeClass(side, "rail-auto")
            }
            // Runs before LiveView's own click handler, so it reads the state the toggle leaves.
            toggle.addEventListener("click", () => store(!side.classList.contains("rail")))
            // On a phone the open sidebar is a drawer: a pick folds it, and the next page opens folded.
            side.addEventListener("click", e => {
              if (phone() && e.target.closest("#tree a") && !side.classList.contains("rail")) toggle.click()
            })
            this.onKey = e => {
              if (e.key !== "[" || e.ctrlKey || e.metaKey || e.altKey) return
              if (e.target.closest("input, textarea, select, [contenteditable]") || document.querySelector("dialog[open]")) return
              e.preventDefault()
              toggle.click()
            }
            window.addEventListener("keydown", this.onKey)
          },
          destroyed() { window.removeEventListener("keydown", this.onKey) }
        }
      </script>
      <main class="panel">{render_slot(@inner_block)}</main>
      <.flash_group flash={@flash} />
    </div>

    <div :if={@sidebar == []} id="app-shell">
      <header class="app-header">
        <div class="shell-content app-topbar">
          <a href={~p"/"} class="app-brand">
            <span class="app-mark"><.icon name="hero-folder-open" class="size-4" /></span> SlimServe
          </a>
          <div class="app-session">
            <span :if={!@login_page} class="session-label">
              {cond do
                @write? -> "Editor"
                @logout? -> "Viewer"
                true -> "Guest"
              end}
            </span>
            <a :if={@edit_login?} href={~p"/.app/login"} id="edit-login" class="btn">Log in to edit</a>
            <.form
              :if={@logout?}
              for={to_form(%{})}
              action={~p"/.app/logout"}
              id="logout-form"
            >
              <button type="submit" class="btn">Log out</button>
            </.form>
          </div>
        </div>
      </header>

      <main class="shell-content app-main">
        {render_slot(@inner_block)}
      </main>

      <.flash_group flash={@flash} />
    </div>
    """
  end

  attr :flash, :map, required: true, doc: "the map of flash messages"
  attr :id, :string, default: "flash-group", doc: "the optional id of flash container"

  def flash_group(assigns) do
    ~H"""
    <div id={@id} aria-live="polite">
      <.flash kind={:info} flash={@flash} />
      <.flash kind={:error} flash={@flash} />

      <.flash
        id="client-error"
        kind={:error}
        title="We can't find the internet"
        phx-disconnected={
          show(".phx-client-error #client-error")
          |> JS.remove_attribute("hidden", to: ".phx-client-error #client-error")
        }
        phx-connected={hide("#client-error") |> JS.set_attribute({"hidden", ""})}
        hidden
      >
        Attempting to reconnect
        <.icon name="hero-arrow-path" class="ml-1 size-3 motion-safe:animate-spin" />
      </.flash>

      <.flash
        id="server-error"
        kind={:error}
        title="Something went wrong!"
        phx-disconnected={
          show(".phx-server-error #server-error")
          |> JS.remove_attribute("hidden", to: ".phx-server-error #server-error")
        }
        phx-connected={hide("#server-error") |> JS.set_attribute({"hidden", ""})}
        hidden
      >
        Attempting to reconnect
        <.icon name="hero-arrow-path" class="ml-1 size-3 motion-safe:animate-spin" />
      </.flash>
    </div>
    """
  end
end
