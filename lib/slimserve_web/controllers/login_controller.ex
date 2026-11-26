# Reached only with login or a write password on, because SlimServeWeb.Login answers 404 otherwise.
defmodule SlimServeWeb.LoginController do
  use SlimServeWeb, :controller

  alias SlimServe.LoginLimit
  alias SlimServeWeb.Login

  plug :put_format, "html"

  def new(conn, params), do: form(conn, 200, params["next"], nil)

  def create(conn, params) do
    with {:ok, attempt} <- LoginLimit.attempt(conn.remote_ip),
         grant when grant != false <- Login.grant(params["username"], params["password"]) do
      LoginLimit.forget(attempt)

      conn
      |> configure_session(renew: true)
      |> Login.put_grant(grant)
      |> redirect(to: local(params["next"]))
    else
      :limited -> send_resp(conn, 429, "Too Many Requests")
      false -> form(conn, 401, params["next"], if(Login.on?(), do: "Wrong username or password", else: "Wrong password"))
    end
  end

  # Closes this browser's open pages: they remount through the checks, without the dropped session.
  def delete(conn, _params) do
    if socket_id = get_session(conn, :live_socket_id), do: SlimServeWeb.Endpoint.broadcast(socket_id, "disconnect", %{})

    conn |> configure_session(drop: true) |> redirect(to: ~p"/.app/login")
  end

  defp form(conn, status, next, error) do
    conn
    |> put_status(status)
    |> render(:new,
      form: Phoenix.Component.to_form(%{"next" => next}),
      error: error,
      username?: Login.on?(),
      page_title: "Log in"
    )
  end

  # Phoenix's own rule: a hand-rolled "starts with / and not //" accepts /\\evil.com.
  defp local(next) when is_binary(next) do
    if Phoenix.URL.classify_local_path(next) == :ok, do: next, else: "/"
  end

  defp local(_next), do: "/"
end
