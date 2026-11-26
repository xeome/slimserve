defmodule SlimServe.RuntimeConfigTest do
  # Not async: the variables are the process-wide environment.
  use ExUnit.Case, async: false

  @variables ~w(SLIMSERVE_ROOT PHX_SERVER SLIMSERVE_USERNAME SLIMSERVE_PASSWORD SECRET_KEY_BASE SLIMSERVE_WRITE_PASSWORD SLIMSERVE_UPLOAD_MAX_MB SLIMSERVE_UPLOAD_TYPES SLIMSERVE_HTTPS PHX_HOST PORT)

  setup do
    saved = Map.new(@variables, &{&1, System.get_env(&1)})

    on_exit(fn -> Enum.each(saved, fn {name, value} -> set(name, value) end) end)
  end

  defp set(name, nil), do: System.delete_env(name)
  defp set(name, value), do: System.put_env(name, value)

  defp prod_root(root, server) do
    prod(%{"SLIMSERVE_ROOT" => root, "PHX_SERVER" => server})[:root]
  end

  defp prod_login(user, pass, secret) do
    prod(%{"SLIMSERVE_USERNAME" => user, "SLIMSERVE_PASSWORD" => pass, "SECRET_KEY_BASE" => secret})[:login]
  end

  defp prod(vars) do
    for name <- @variables, do: set(name, vars[name])

    "config/runtime.exs" |> Config.Reader.read!(env: :prod) |> Keyword.fetch!(:slimserve)
  end

  test "Scenario: SLIMSERVE_WRITE_PASSWORD sets the write password, and an empty one counts as unset" do
    assert prod(%{"SLIMSERVE_WRITE_PASSWORD" => "w"})[:write_password] == "w"
    assert prod(%{"SLIMSERVE_WRITE_PASSWORD" => ""})[:write_password] == nil
    assert prod(%{})[:write_password] == nil
  end

  test "Scenario: Uploads default to 100 MB of any type" do
    assert prod(%{})[:upload_max_bytes] == 100 * 1024 * 1024
    assert prod(%{})[:upload_types] == :any
    assert prod(%{"SLIMSERVE_UPLOAD_MAX_MB" => "5"})[:upload_max_bytes] == 5 * 1024 * 1024
  end

  test "Scenario: SLIMSERVE_UPLOAD_TYPES takes extensions with or without their leading dot" do
    assert prod(%{"SLIMSERVE_UPLOAD_TYPES" => "txt"})[:upload_types] == [".txt"]
    assert prod(%{"SLIMSERVE_UPLOAD_TYPES" => "txt, .png"})[:upload_types] == [".txt", ".png"]
  end

  # An upload's extension is downcased before it is looked up.
  test "Scenario: SLIMSERVE_UPLOAD_TYPES entries match in any case" do
    assert prod(%{"SLIMSERVE_UPLOAD_TYPES" => "JPG"})[:upload_types] == [".jpg"]
  end

  # An empty list would make allow_upload accept nothing, which no one sets on purpose.
  test "Scenario: An SLIMSERVE_UPLOAD_TYPES that lists no extension refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_UPLOAD_TYPES/, fn -> prod(%{"SLIMSERVE_UPLOAD_TYPES" => ","}) end
  end

  # A viewer would then hold write rights.
  test "Scenario: A write password equal to the viewer password refuses to boot" do
    login = %{"SLIMSERVE_USERNAME" => "u", "SLIMSERVE_PASSWORD" => "p", "SECRET_KEY_BASE" => "s"}

    assert_raise RuntimeError, ~r/SLIMSERVE_WRITE_PASSWORD/, fn ->
      prod(Map.put(login, "SLIMSERVE_WRITE_PASSWORD", "p"))
    end

    assert prod(Map.put(login, "SLIMSERVE_WRITE_PASSWORD", "w"))[:write_password] == "w"
  end

  # allow_upload raises on such an extension, so the boot refuses it first.
  test "Scenario: An upload type with no known MIME type refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_UPLOAD_TYPES/, fn -> prod(%{"SLIMSERVE_UPLOAD_TYPES" => "txt,notatype"}) end
  end

  @tag :tmp_dir
  test "Scenario: A server boots only when SLIMSERVE_ROOT names an existing directory", %{tmp_dir: dir} do
    File.write!(Path.join(dir, "file"), "")

    assert prod_root(dir, "true") == dir
    assert_raise RuntimeError, fn -> prod_root(nil, "true") end
    assert_raise RuntimeError, fn -> prod_root(Path.join(dir, "missing"), "true") end
    assert_raise RuntimeError, fn -> prod_root(Path.join(dir, "file"), "true") end
  end

  test "Scenario: A task that starts no server loads without a root" do
    assert prod_root(nil, nil) == File.cwd!()
  end

  test "Scenario: Login is on when both SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD are set, and off when neither is" do
    assert prod_login("u", "p", "secret") == {"u", "p"}
    assert prod_login(nil, nil, nil) == nil
  end

  test "Scenario: Only one of SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login("u", nil, "secret") end
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login(nil, "p", "secret") end
  end

  test "Scenario: An empty SLIMSERVE_PASSWORD beside a set SLIMSERVE_USERNAME refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login("u", "", "secret") end
  end

  test "Scenario: Both login variables empty leave login off" do
    assert prod_login("", "", nil) == nil
  end

  test "Scenario: Login in prod without SECRET_KEY_BASE refuses to boot" do
    assert_raise RuntimeError, ~r/SECRET_KEY_BASE/, fn -> prod_login("u", "p", nil) end
  end

  test "Scenario: Production advertises the URL for each mode without changing the HTTP listener" do
    https =
      prod(%{"SLIMSERVE_HTTPS" => "true", "PHX_HOST" => "files.example.test", "PORT" => "4081"})[SlimServeWeb.Endpoint]

    assert https[:url] == [host: "files.example.test", port: 443, scheme: "https"]
    assert https[:http][:port] == 4081

    endpoint = prod(%{"PORT" => "4081"})[SlimServeWeb.Endpoint]

    assert endpoint[:http][:port] == 4081
    assert endpoint[:url][:port] == 4081
    assert endpoint[:url][:scheme] == "http"
  end

  test "Scenario: HTTPS requires a nonempty PHX_HOST" do
    for host <- [nil, ""] do
      assert_raise RuntimeError, ~r/PHX_HOST/, fn ->
        prod(%{"SLIMSERVE_HTTPS" => "true", "PHX_HOST" => host})
      end
    end
  end

  test "Scenario: A nonempty SLIMSERVE_HTTPS value other than true or false refuses to boot" do
    for value <- ["1", "TRUE", "yes", "on"] do
      assert_raise RuntimeError, ~r/SLIMSERVE_HTTPS/, fn -> prod(%{"SLIMSERVE_HTTPS" => value}) end
    end
  end
end
