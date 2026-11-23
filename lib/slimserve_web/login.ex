defmodule SlimServeWeb.Login do
  @moduledoc """
  The login gate, on when `config :slimserve, :login` is `{username, password}`.

  As a plug in the :serve pipeline it lets a request through only when its session holds the
  digest of the configured login; as an `on_mount` hook it refuses a LiveView socket the same way,
  because the socket is mounted in the endpoint, ahead of the router.
  """
  @behaviour Plug

  use SlimServeWeb, :verified_routes

  import Phoenix.Controller, only: [current_path: 1, redirect: 2]
  import Plug.Conn

  @session_key "login"

  def on?, do: Application.get_env(:slimserve, :login) != nil

  @doc "Whether the given username and password are the configured ones. Both comparisons always run."
  def valid?(user, pass) when is_binary(user) and is_binary(pass) do
    {want_user, want_pass} = Application.fetch_env!(:slimserve, :login)
    # Equal-length digests, so the comparison time does not leak the lengths.
    user_ok = Plug.Crypto.secure_compare(sha256(user), sha256(want_user))
    pass_ok = Plug.Crypto.secure_compare(sha256(pass), sha256(want_pass))
    user_ok and pass_ok
  end

  def valid?(_user, _pass), do: false

  defp sha256(text), do: :crypto.hash(:sha256, text)

  def put_login(conn), do: put_session(conn, @session_key, digest())

  # The cookie is signed but readable, so the digest is keyed with the server secret: an unkeyed
  # hash of the password could be cracked offline. A new password logs every session out.
  defp digest do
    {user, pass} = Application.fetch_env!(:slimserve, :login)
    secret = SlimServeWeb.Endpoint.config(:secret_key_base)
    :crypto.mac(:hmac, :sha256, secret, user <> <<0>> <> pass)
  end

  defp logged_in?(session), do: Plug.Crypto.secure_compare(session[@session_key] || "", digest())

  @impl Plug
  def init(opts), do: opts

  @impl Plug
  def call(conn, _opts), do: if(on?(), do: gate(conn), else: off(conn))

  defp off(%{path_info: [".app", page]} = conn) when page in ~w(login logout), do: not_found(conn)
  defp off(conn), do: conn

  # Logout passes too, so a session the gate no longer accepts can still clear its cookie.
  defp gate(%{path_info: [".app", page]} = conn) when page in ~w(login logout), do: conn

  defp gate(conn) do
    cond do
      logged_in?(get_session(conn)) ->
        conn

      conn.method == "GET" and html?(conn) ->
        conn |> redirect(to: ~p"/.app/login?#{[next: current_path(conn)]}") |> halt()

      true ->
        conn |> send_resp(401, "Unauthorized") |> halt()
    end
  end

  def on_mount(:default, _params, session, socket) do
    if not on?() or logged_in?(session),
      do: {:cont, socket},
      else: {:halt, Phoenix.LiveView.redirect(socket, to: ~p"/.app/login")}
  end

  defp html?(conn), do: conn |> get_req_header("accept") |> Enum.any?(&String.contains?(&1, "text/html"))

  defp not_found(conn), do: conn |> send_resp(404, "Not Found") |> halt()
end
