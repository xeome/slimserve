defmodule SlimServeWeb.Login do
  @moduledoc """
  The login gate, on when `config :slimserve, :login` is `{username, password}`, and the write
  rights, on when `config :slimserve, :write_password` is set.

  As a plug in the :serve pipeline it lets a request through only when its session holds the
  digest of the configured login; as an `on_mount` hook it refuses a LiveView socket the same way,
  because the socket is mounted in the endpoint, ahead of the router. `plug SlimServeWeb.Login, :write`
  and `on_mount {SlimServeWeb.Login, :write}` do the same for write rights.
  """
  @behaviour Plug

  use SlimServeWeb, :verified_routes

  import Phoenix.Controller, only: [current_path: 1, redirect: 2]
  import Plug.Conn

  alias SlimServeWeb.BrowseHTML

  @session_key "login"
  @write_key "write"

  def on?, do: Application.get_env(:slimserve, :login) != nil

  def write_on?, do: Application.get_env(:slimserve, :write_password) not in [nil, ""]

  @doc """
  What a login form's username and password grant: `:write` for the write password, `:login` for
  the viewer one, or `false`. With viewer login on, the username must match too; with it off, the
  username is not asked for. Every configured comparison runs.
  """
  def grant(user, pass) when is_binary(pass) do
    {want_user, want_pass} = Application.get_env(:slimserve, :login) || {nil, nil}
    user_ok = want_user == nil or same?(user, want_user)
    viewer_ok = want_pass != nil and same?(pass, want_pass)
    write_ok = write_on?() and same?(pass, Application.fetch_env!(:slimserve, :write_password))

    cond do
      not user_ok -> false
      write_ok -> :write
      viewer_ok -> :login
      true -> false
    end
  end

  def grant(_user, _pass), do: false

  # Equal-length digests, so the comparison time does not leak the lengths.
  defp same?(given, wanted), do: is_binary(given) and Plug.Crypto.secure_compare(sha256(given), sha256(wanted))

  defp sha256(text), do: :crypto.hash(:sha256, text)

  @doc """
  Puts a grant in the session. A write grant with viewer login on carries the login too, and names
  the browser's sockets, so a logout can close the open pages that hold write rights. A second
  write login keeps the name, because the pages opened after the first one carry it.
  """
  # A viewer login replaces a write grant from an earlier login in the same browser.
  def put_grant(conn, :login), do: conn |> delete_session(@write_key) |> put_session(@session_key, digest())

  def put_grant(conn, :write) do
    conn = if on?(), do: put_grant(conn, :login), else: conn

    socket_id =
      get_session(conn, :live_socket_id) ||
        "write_socket:" <> Base.url_encode64(:crypto.strong_rand_bytes(16), padding: false)

    conn |> put_session(@write_key, write_digest()) |> put_session(:live_socket_id, socket_id)
  end

  # The cookie is signed but readable, so the digest is keyed with the server secret: an unkeyed
  # hash of the password could be cracked offline. A new password logs every session out.
  defp digest do
    {user, pass} = Application.fetch_env!(:slimserve, :login)
    mac(user <> <<0>> <> pass)
  end

  defp write_digest, do: mac("write" <> <<0>> <> Application.fetch_env!(:slimserve, :write_password))

  defp mac(text), do: :crypto.mac(:hmac, :sha256, SlimServeWeb.Endpoint.config(:secret_key_base), text)

  defp logged_in?(session), do: Plug.Crypto.secure_compare(session[@session_key] || "", digest())

  @doc "Whether a session holds write rights: a write password is set, the session's digest is of it, and any login holds too."
  def write?(session) do
    write_on?() and Plug.Crypto.secure_compare(session[@write_key] || "", write_digest()) and
      (not on?() or logged_in?(session))
  end

  @impl Plug
  def init(opts), do: opts

  @impl Plug
  def call(conn, :write) do
    cond do
      not write_on?() -> BrowseHTML.send_not_found(conn)
      write?(get_session(conn)) -> conn
      true -> refuse(conn)
    end
  end

  def call(conn, _opts), do: if(on?(), do: gate(conn), else: off(conn))

  defp off(%{path_info: [".app", page]} = conn) when page in ~w(login logout),
    do: if(write_on?(), do: conn, else: BrowseHTML.send_not_found(conn))

  defp off(conn), do: conn

  # Logout passes too, so a session the gate no longer accepts can still clear its cookie.
  defp gate(%{path_info: [".app", page]} = conn) when page in ~w(login logout), do: conn

  defp gate(conn), do: if(logged_in?(get_session(conn)), do: conn, else: refuse(conn))

  defp refuse(conn) do
    if conn.method == "GET" and BrowseHTML.accepts_html?(conn),
      do: conn |> redirect(to: ~p"/.app/login?#{[next: current_path(conn)]}") |> halt(),
      else: conn |> send_resp(401, "Unauthorized") |> halt()
  end

  def on_mount(:default, _params, session, socket) do
    if not on?() or logged_in?(session), do: {:cont, socket}, else: to_login(socket)
  end

  def on_mount(:write, _params, session, socket) do
    if write?(session), do: {:cont, socket}, else: to_login(socket)
  end

  defp to_login(socket), do: {:halt, Phoenix.LiveView.redirect(socket, to: ~p"/.app/login")}
end
