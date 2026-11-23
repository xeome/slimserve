defmodule SlimServe.RuntimeConfigTest do
  # Not async: the variables are the process-wide environment.
  use ExUnit.Case, async: false

  setup do
    saved = Map.new(~w(SLIMSERVE_ROOT PHX_SERVER), &{&1, System.get_env(&1)})
    on_exit(fn -> Enum.each(saved, fn {name, value} -> set(name, value) end) end)
  end

  defp set(name, nil), do: System.delete_env(name)
  defp set(name, value), do: System.put_env(name, value)

  defp prod_root(root, server) do
    set("SLIMSERVE_ROOT", root)
    set("PHX_SERVER", server)
    "config/runtime.exs" |> Config.Reader.read!(env: :prod) |> get_in([:slimserve, :root])
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
end
