defmodule SlimServeWeb.HealthTest do
  # Not async: login, storage and the HTTPS policy are application env.
  use SlimServeWeb.ConnCase, async: false

  setup do
    on_exit(fn -> for key <- [:login, :storage, :s3, :ssl_redirect], do: Application.delete_env(:slimserve, key) end)
    Application.put_env(:slimserve, :login, {"user", "pass"})
  end

  test "Scenario: The health paths answer 200 without login and with S3 unreachable", %{conn: conn} do
    # Nothing listens on port 1, so any S3 request would fail.
    Application.put_env(:slimserve, :storage, :s3)
    Application.put_env(:slimserve, :s3, SlimServe.S3Fixture.config("unreachable", "http://127.0.0.1:1"))

    assert conn |> get("/.app/healthz") |> response(200) == "ok"
    assert conn |> get("/.app/readyz") |> response(200) == "ok"
    assert conn |> get("/") |> response(401)
  end

  test "Scenario: With HTTPS on, a plain-HTTP health probe is answered, not redirected", %{conn: conn} do
    Application.put_env(:slimserve, :ssl_redirect, Plug.SSL.init(host: "files.example.test"))

    assert conn |> get("/") |> response(301)
    assert conn |> get("/.app/readyz") |> response(200) == "ok"

    # A proxy may probe with HEAD.
    for path <- ["/.app/healthz", "/.app/readyz"] do
      assert conn |> head(path) |> response(200) == ""
    end
  end
end
