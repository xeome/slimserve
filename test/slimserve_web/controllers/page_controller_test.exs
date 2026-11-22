defmodule SlimServeWeb.PageControllerTest do
  use SlimServeWeb.ConnCase

  test "GET /", %{conn: conn} do
    conn = get(conn, ~p"/")
    assert html_response(conn, 200) =~ "SlimServe is being rebuilt on Phoenix."
  end

  test "GET / sends the app CSP without the dashboard's nonce", %{conn: conn} do
    [csp] = conn |> get(~p"/") |> get_resp_header("content-security-policy")

    assert csp =~ "base-uri 'self'"
    assert csp =~ "frame-ancestors 'self'"
    refute csp =~ "nonce-"
  end
end
