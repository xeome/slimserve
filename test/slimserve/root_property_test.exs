defmodule SlimServe.RootPropertyTest do
  use ExUnit.Case, async: true
  use ExUnitProperties

  alias SlimServe.Root

  # Names in the fixture root. Every link_* has a visible name; its target may not.
  @names ~w(visible.txt sub inner.txt ignored.txt .secret .hidden .slimserveignore
            link_visible link_sub link_dot link_dotdir link_ignored link_out link_abs)

  @tag :tmp_dir
  property "Scenario: Any request path resolves only to a visible path under the root",
           %{tmp_dir: tmp} do
    root = Path.join(tmp, "root")
    outside = Path.join(tmp, "outside")
    File.mkdir_p!(Path.join(root, "sub"))
    File.mkdir_p!(Path.join(root, ".hidden"))
    File.mkdir_p!(outside)

    for file <- ~w(visible.txt sub/inner.txt ignored.txt .secret .hidden/inner.txt),
        do: File.write!(Path.join(root, file), "x")

    File.write!(Path.join(outside, "inner.txt"), "x")
    File.write!(Path.join(root, ".slimserveignore"), "ignored.txt\n")

    links = [
      link_visible: "visible.txt",
      link_sub: "sub",
      link_dot: ".secret",
      link_dotdir: ".hidden",
      link_ignored: "ignored.txt",
      link_out: "../outside",
      link_abs: Path.join(root, "visible.txt")
    ]

    for {name, target} <- links, do: File.ln_s!(target, Path.join(root, Atom.to_string(name)))

    real_root = realpath(root)

    check all(segments <- list_of(segment(), max_length: 4), max_runs: 1000) do
      case Root.resolve(root, segments) do
        :error ->
          :ok

        {kind, path} when kind in [:dir, :file] ->
          real = realpath(path)
          assert real == real_root or String.starts_with?(real, real_root <> "/"), inspect({segments, real})
          components = if real == real_root, do: [], else: Path.split(Path.relative_to(real, real_root))
          refute Enum.any?(components, &String.starts_with?(&1, ".")), inspect({segments, real})
          refute "ignored.txt" in components, inspect({segments, real})
      end
    end
  end

  defp segment do
    name = member_of(@names)

    frequency([
      {6, name},
      {1, member_of([".", ".."])},
      {1, map({name, name, member_of(["/", "%2F"])}, fn {a, b, sep} -> a <> sep <> b end)},
      {1, binary(min_length: 1)},
      {1, map(name, &(&1 <> <<0>>))},
      {1, map(name, &(&1 <> <<0xFF>>))}
    ])
  end

  defp realpath(path) do
    {out, 0} = System.cmd("realpath", ["-e", path])
    String.trim_trailing(out, "\n")
  end
end
