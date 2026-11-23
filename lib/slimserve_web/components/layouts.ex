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

  slot :inner_block, required: true

  # Every page behind the login gate is logged in by construction, so the button shows whenever login is on.
  # With only a write password, viewing needs no login, so the header offers one for editing instead.
  def app(assigns) do
    assigns =
      assign(assigns,
        logout?: (Login.on?() or assigns.write?) and not assigns.login_page,
        edit_login?: Login.write_on?() and not Login.on?() and not assigns.write? and not assigns.login_page
      )

    ~H"""
    <header>
      <a href={~p"/"} class="name">SlimServe</a>
      <span class="meta">Phoenix {Application.spec(:phoenix, :vsn)}</span>
      <a :if={@edit_login?} href={~p"/.app/login"} id="edit-login" class="btn logout">Log in to edit</a>
      <.form
        :if={@logout?}
        for={to_form(%{})}
        action={~p"/.app/logout"}
        id="logout-form"
        class="logout"
      >
        <button type="submit" class="btn">Log out</button>
      </.form>
    </header>

    <main>
      {render_slot(@inner_block)}
    </main>

    <.flash_group flash={@flash} />
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
