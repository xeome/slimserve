defmodule SlimServe.Root do
  @moduledoc """
  Every download and every listing entry goes through `resolve/2`, so a path it refuses
  is neither served nor listed. Its rules apply to both the requested names and the names
  they resolve to through symlinks, so a link with a visible name cannot serve a hidden path.
  """

  alias SlimServe.Ignore

  @doc """
  Answers `:error` for a path that leaves the root, has a component that starts with "." or
  is not valid UTF-8, is ignored by a `.slimserveignore` file, does not exist, or is a file this process cannot
  read. The answer never says which.
  """
  @spec resolve(Path.t(), [String.t()]) :: {:dir, Path.t()} | {:file, Path.t()} | :error
  def resolve(root, []), do: {:dir, root}

  def resolve(root, segments) do
    # safe_relative/2 follows every symlink on the way and refuses one that is absolute or climbs out of root.
    with true <- Enum.all?(segments, &name?/1),
         {:ok, relative} <- Path.safe_relative(Path.join(segments), root),
         resolved = Path.split(relative),
         true <- Enum.all?(resolved, &name?/1),
         path = Path.join(root, relative),
         {:ok, %File.Stat{type: type, access: access}} <- File.stat(path),
         false <- Enum.any?(Enum.uniq([segments, resolved]), &Ignore.ignored?(root, &1)) do
      case type do
        :directory -> {:dir, path}
        # An unreadable directory needs no rule: its ignore file cannot be read, so Ignore hides it.
        :regular when access in [:read, :read_write] -> {:file, path}
        _ -> :error
      end
    else
      _ -> :error
    end
  end

  @doc """
  The one gate every write goes through.

  Answers `{:ok, abs}` only when `parent` resolves to a directory, `name` is one plain name, and the
  name is not ignored at its requested path or the path it resolves to. `abs` is the name itself, so
  a link there is the link, never its target. `dir?: true` says a name that does not exist yet is to
  be a folder, so a dir-only pattern applies to it. Whether the name exists is the caller's question.
  """
  @spec target(Path.t(), [String.t()], String.t(), dir?: boolean()) :: {:ok, Path.t()} | :error
  def target(root, parent, name, opts \\ []) do
    requested = parent ++ [name]

    # safe_relative/2 keeps a missing leaf and follows a parent link. Path.relative_to/2 of the
    # directory would give "." at the root, which no negation in a catch-all ignore file matches.
    with {:dir, dir} <- resolve(root, parent),
         true <- name?(name),
         {:ok, relative} <- Path.safe_relative(Path.join(requested), root),
         false <- Enum.any?(Enum.uniq([requested, Path.split(relative)]), &Ignore.ignored?(root, &1, opts)) do
      {:ok, Path.join(dir, name)}
    else
      _ -> :error
    end
  end

  @spec topic(Path.t()) :: String.t()
  def topic(rel_dir) when rel_dir in ["", "."], do: "dir:"
  def topic(rel_dir), do: "dir:" <> rel_dir

  @doc """
  Phoenix decodes %2F into a segment, so "sub/.env" can arrive as a single segment; the leading-dot
  rule also covers "." and "..". A name that is not valid UTF-8 would crash the ignore patterns' match.
  A NUL byte is refused here, before any S3 key is built from it; File.stat/1 would refuse it too.
  """
  @spec name?(String.t()) :: boolean()
  def name?(segment),
    do:
      segment != "" and String.valid?(segment) and not String.starts_with?(segment, ".") and
        not String.contains?(segment, ["/", <<0>>])
end
