defmodule SlimServe.RuntimeConfigTest do
  # Not async: the variables are the process-wide environment.
  use ExUnit.Case, async: false

  setup do
    saved =
      Map.new(
        ~w(SLIMSERVE_ROOT PHX_SERVER SLIMSERVE_USERNAME SLIMSERVE_PASSWORD SECRET_KEY_BASE),
        &{&1, System.get_env(&1)}
      )

    on_exit(fn -> Enum.each(saved, fn {name, value} -> set(name, value) end) end)
  end

  defp set(name, nil), do: System.delete_env(name)
  defp set(name, value), do: System.put_env(name, value)

  defp prod_root(root, server) do
    set("SLIMSERVE_ROOT", root)
    set("PHX_SERVER", server)
    "config/runtime.exs" |> Config.Reader.read!(env: :prod) |> get_in([:slimserve, :root])
  end

  defp prod_login(user, pass, secret) do
    set("PHX_SERVER", nil)
    set("SLIMSERVE_USERNAME", user)
    set("SLIMSERVE_PASSWORD", pass)
    set("SECRET_KEY_BASE", secret)
    "config/runtime.exs" |> Config.Reader.read!(env: :prod) |> get_in([:slimserve, :login])
  end

  @tag :tmp_dir
  test "a prod server boots only when SLIMSERVE_ROOT names an existing directory", %{tmp_dir: dir} do
    File.write!(Path.join(dir, "file"), "")

    assert prod_root(dir, "true") == dir
    assert_raise RuntimeError, fn -> prod_root(nil, "true") end
    assert_raise RuntimeError, fn -> prod_root(Path.join(dir, "missing"), "true") end
    assert_raise RuntimeError, fn -> prod_root(Path.join(dir, "file"), "true") end
  end

  test "prod config loads without a root when PHX_SERVER is unset, as for a mix task" do
    assert prod_root(nil, nil) == File.cwd!()
  end

  test "login is on when both SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD are set, and off when neither is" do
    assert prod_login("u", "p", "secret") == {"u", "p"}
    assert prod_login(nil, nil, nil) == nil
  end

  test "only one of SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login("u", nil, "secret") end
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login(nil, "p", "secret") end
  end

  test "an empty SLIMSERVE_PASSWORD beside a set SLIMSERVE_USERNAME refuses to boot" do
    assert_raise RuntimeError, ~r/SLIMSERVE_USERNAME and SLIMSERVE_PASSWORD/, fn -> prod_login("u", "", "secret") end
  end

  test "both empty leaves login off" do
    assert prod_login("", "", nil) == nil
  end

  test "login in prod without SECRET_KEY_BASE refuses to boot" do
    assert_raise RuntimeError, ~r/SECRET_KEY_BASE/, fn -> prod_login("u", "p", nil) end
  end
end
