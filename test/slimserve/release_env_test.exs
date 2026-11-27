defmodule SlimServe.ReleaseEnvTest do
  use ExUnit.Case, async: true

  @script ~s(. rel/env.sh.eex && echo "$RELEASE_DISTRIBUTION")

  defp source(distribution, cookie, script \\ @script) do
    env = [{"RELEASE_DISTRIBUTION", distribution}, {"RELEASE_COOKIE", cookie}]
    {out, status} = System.cmd("sh", ["-c", script], env: env, stderr_to_stdout: true)
    {String.trim(out), status}
  end

  test "an unset distribution becomes none" do
    assert source(nil, nil) == {"none", 0}
  end

  test "an empty distribution becomes none" do
    # An Erlang port drops an empty env value, so the shell sets it.
    assert source(nil, nil, "export RELEASE_DISTRIBUTION=; " <> @script) == {"none", 0}
  end

  test "name with a cookie keeps name" do
    assert source("name", "secret") == {"name", 0}
  end

  test "sname with no cookie refuses to start" do
    {out, status} = source("sname", nil)
    assert status == 1
    assert out =~ "set RELEASE_COOKIE"
  end

  test "a value other than name or sname needs no cookie" do
    assert source("Name", nil) == {"Name", 0}
  end

  test "under set -e, the release script goes on after sourcing the file" do
    assert source("name", "secret", "set -e; . rel/env.sh.eex; echo after") == {"after", 0}
  end
end
