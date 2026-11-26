defmodule SlimServe.RootTargetTest do
  use ExUnit.Case, async: true

  alias SlimServe.Root

  @moduletag :tmp_dir

  setup %{tmp_dir: tmp_dir} do
    root = Path.join(tmp_dir, "root")
    outside = Path.join(tmp_dir, "outside")
    File.mkdir_p!(Path.join(root, "sub"))
    File.mkdir_p!(outside)
    {:ok, root: root, outside: outside}
  end

  defp ignore(root, patterns), do: File.write!(Path.join(root, ".slimserveignore"), patterns)

  test "Scenario: A new name in the root or in a folder is written at that name", %{root: root} do
    assert Root.target(root, [], "new.txt") == {:ok, Path.join(root, "new.txt")}
    assert Root.target(root, ["sub"], "new") == {:ok, Path.join(root, "sub/new")}
  end

  # An empty name would be the folder itself, so a delete of "" would remove the folder being listed.
  test "Scenario: A name that is empty, a dot-name, holds a slash or is not UTF-8 is refused", %{root: root} do
    for name <- ["", ".", "..", ".x", "a/b", <<0xFF>>] do
      assert Root.target(root, [], name) == :error, inspect(name)
      assert Root.target(root, ["sub"], name) == :error, inspect(name)
    end
  end

  test "Scenario: An ignored name is refused, and a dir-only pattern refuses only a folder", %{root: root} do
    ignore(root, "secret.txt\nbuild/\n")

    assert Root.target(root, [], "secret.txt") == :error
    assert Root.target(root, ["sub"], "secret.txt") == :error
    assert Root.target(root, [], "build", dir?: true) == :error
    assert Root.target(root, [], "build") == {:ok, Path.join(root, "build")}
  end

  test "Scenario: A name ignored only at the path it resolves to, through a parent link, is refused", %{root: root} do
    File.mkdir_p!(Path.join(root, "real"))
    File.ln_s!("real", Path.join(root, "link"))
    ignore(root, "/real/secret\n")

    assert Root.target(root, ["link"], "secret") == :error
    assert Root.target(root, ["real"], "secret") == :error
    assert Root.target(root, ["link"], "other") == {:ok, Path.join(root, "real/other")}
  end

  test "Scenario: A name ignored only at its requested path, through a parent link, is refused there only", %{root: root} do
    File.mkdir_p!(Path.join(root, "real"))
    File.ln_s!("real", Path.join(root, "link"))
    ignore(root, "/link/secret\n")

    assert Root.target(root, ["link"], "secret") == :error
    assert {:ok, _abs} = Root.target(root, ["real"], "secret")
  end

  test "Scenario: A root-level name kept by a negation under a catch-all ignore file is allowed", %{root: root} do
    ignore(root, "*\n!keep.txt\n")

    assert Root.target(root, [], "keep.txt") == {:ok, Path.join(root, "keep.txt")}
    assert Root.target(root, [], "other.txt") == :error
  end

  test "Scenario: A parent that is a dot-dir, ignored, missing, a file or a link out of the root is refused", %{
    root: root,
    outside: outside
  } do
    File.mkdir_p!(Path.join(root, ".hidden"))
    File.mkdir_p!(Path.join(root, "skipped"))
    ignore(root, "skipped\n")
    File.write!(Path.join(root, "file.txt"), "x")
    File.ln_s!(outside, Path.join(root, "out"))

    for parent <- [[".hidden"], ["skipped"], ["missing"], ["file.txt"], ["out"]] do
      assert Root.target(root, parent, "x") == :error, inspect(parent)
    end
  end

  test "Scenario: A write to an existing link acts on the link, never its target", %{root: root} do
    File.write!(Path.join(root, "sub/a.txt"), "x")
    File.ln_s!("sub", Path.join(root, "to-dir"))
    File.ln_s!("sub/a.txt", Path.join(root, "to-file"))

    assert Root.target(root, [], "to-dir") == {:ok, Path.join(root, "to-dir")}
    assert Root.target(root, [], "to-file") == {:ok, Path.join(root, "to-file")}
  end
end
