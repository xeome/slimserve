defmodule SlimServe.Root do
  @moduledoc """
  The one gate between a request path and the file system.

  Every download and every listing entry goes through `resolve/2`, so a path it refuses
  is neither served nor listed. Its rules apply to both the requested names and the names
  they resolve to through symlinks, so a link with a visible name cannot serve a hidden path.
  """

  alias SlimServe.Ignore

  @doc """
  Resolves the decoded path segments of a request to a directory or a file under `root`.

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

  # One name of one directory entry. Phoenix decodes %2F into a segment, so "sub/.env" can arrive
  # as a single segment; the leading-dot rule also covers "." and "..". A name that is not valid
  # UTF-8 would crash the ignore patterns' match. A NUL byte needs no rule: File.stat/1 refuses
  # it with :badarg.
  defp name?(segment),
    do: String.valid?(segment) and not String.starts_with?(segment, ".") and not String.contains?(segment, "/")
end
