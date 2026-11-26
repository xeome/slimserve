defmodule SlimServe.IgnoreTest do
  use ExUnit.Case, async: true

  alias SlimServe.Ignore

  @tag :tmp_dir
  test "Scenario: Patterns anchor, match wildcards and take other characters literally", %{tmp_dir: root} do
    File.write!(Path.join(root, ".slimserveignore"), """
    # a comment, then a blank line

    *.log
    /top.txt
    doc/*.md
    **/cache
    build/**
    a?c
    [x].txt
    deep/***/leaf
    """)

    cases = [
      {"x.log", true},
      {"sub/x.log", true},
      {"x.logs", false},
      {"xlog", false},
      {"top.txt", true},
      {"sub/top.txt", false},
      {"doc/a.md", true},
      {"doc/sub/a.md", false},
      {"x/doc/a.md", false},
      {"cache", true},
      {"a/b/cache", true},
      {"build/x/y", true},
      {"build", false},
      {"abc", true},
      {"aéc", true},
      {"a/c", false},
      {"ac", false},
      {"[x].txt", true},
      {"x.txt", false},
      {"deep/x/y/leaf", true},
      {"# a comment, then a blank line", false}
    ]

    for {path, expected} <- cases do
      assert Ignore.ignored?(root, String.split(path, "/")) == expected, path
    end
  end

  # A folder about to be made does not exist yet, so only the hint says it is one.
  @tag :tmp_dir
  test "Scenario: A directory-only pattern matches a folder not made yet", %{tmp_dir: root} do
    File.write!(Path.join(root, ".slimserveignore"), "build/\n")

    refute Ignore.ignored?(root, ~w(build))
    assert Ignore.ignored?(root, ~w(build), dir?: true)
    assert Ignore.ignored?(root, ~w(sub build), dir?: true)
    refute Ignore.ignored?(root, ~w(other), dir?: true)
  end

  @tag :tmp_dir
  test "Scenario: A double star inside a name matches like a single star", %{tmp_dir: root} do
    File.write!(Path.join(root, ".slimserveignore"), "foo**bar\n")

    refute Ignore.ignored?(root, ~w(foo x bar))
    assert Ignore.ignored?(root, ~w(fooxbar))
  end
end
