defmodule SlimServe.Ignore do
  @moduledoc """
  `.slimserveignore` files: a subset of gitignore.

  A pattern is a glob with `*`, `?` and `**`. A `**` crosses directories only as a whole
  path segment; inside a name it is a `*`. A leading or inner `/` anchors it to the
  ignore file's directory; otherwise it matches a name at any depth below. A trailing `/`
  matches directories only. A leading `!` negates, and the last matching pattern wins.
  Blank lines and lines starting with `#` are skipped. There are no `[...]` classes and
  no backslash escapes: both are literal.
  """

  @file_name ".slimserveignore"

  @doc """
  Whether the path `segments` under `root`, or any directory above it, is ignored.

  A file under an ignored directory stays ignored whatever a `!` pattern says, as in
  gitignore. An ignore file that exists but cannot be read hides its directory and
  everything under it. `dir?: true` says the last name is a folder even though it does not exist
  yet, as for a folder about to be made.
  """
  @spec ignored?(Path.t(), [String.t()], dir?: boolean()) :: boolean()
  def ignored?(root, segments, opts \\ []) do
    dir? = Keyword.get(opts, :dir?, false) or File.dir?(Path.join([root | segments]))
    ignored_by?(&rules(Path.join([root | &1])), segments, dir?)
  end

  @doc """
  `rules_at` answers, for the segments of a folder, its parsed rules
  (`parse/1`), `[]` when it has no ignore file, or `:unreadable`. S3 reads its ignore objects through
  this, so both backends share one walker.
  """
  @spec ignored_by?(([String.t()] -> list() | :unreadable), [String.t()], boolean()) :: boolean()
  def ignored_by?(rules_at, segments, dir?), do: walk(rules_at, [], segments, [], dir?)

  defp walk(rules_at, here, [], _layers, dir?), do: dir? and rules_at.(here) == :unreadable

  defp walk(rules_at, here, [name | rest], layers, dir?) do
    case rules_at.(here) do
      :unreadable ->
        true

      rules ->
        layers = Enum.map(layers, fn {rules, relative} -> {rules, relative <> "/" <> name} end) ++ [{rules, name}]

        matched?(layers, rest != [] or dir?) or walk(rules_at, here ++ [name], rest, layers, dir?)
    end
  end

  # The last matching rule wins: files from the root down, lines from the top down.
  defp matched?(layers, dir?) do
    for {rules, relative} <- layers,
        {regex, negated?, dir_only?} <- rules,
        (dir? or not dir_only?) and Regex.match?(regex, relative),
        reduce: false do
      _verdict -> not negated?
    end
  end

  # ponytail: read and compiled on every call, no cache. Cache by mtime if requests get slow.
  # The directory comes from Root.resolve, so it never leaves the root.
  # sobelow_skip ["Traversal.FileModule"]
  defp rules(dir) do
    case File.read(Path.join(dir, @file_name)) do
      {:ok, body} -> parse(body)
      # :enotdir: `dir` is a file, the last name of a path.
      {:error, reason} when reason in [:enoent, :enotdir] -> []
      {:error, _reason} -> :unreadable
    end
  end

  @spec parse(binary()) :: list() | :unreadable
  def parse(body), do: if(String.valid?(body), do: compile(body), else: :unreadable)

  # Patterns come from ignore files only the host owner writes, escaped and fully anchored.
  # sobelow_skip ["DOS.Regex"]
  defp compile(body) do
    # Editors on Windows put a byte-order mark first; left in, it would be part of the first pattern.
    for line <- body |> String.replace_prefix(<<0xEF, 0xBB, 0xBF>>, "") |> String.split(["\r\n", "\n"]),
        line = String.trim_trailing(line),
        line != "" and not String.starts_with?(line, "#") do
      {negated?, line} =
        case line do
          "!" <> rest -> {true, rest}
          _ -> {false, line}
        end

      dir_only? = String.ends_with?(line, "/")
      glob = String.trim_trailing(line, "/")
      prefix = if String.contains?(glob, "/"), do: "", else: "(?:.*/)?"
      {Regex.compile!("\\A" <> prefix <> regex(String.trim_leading(glob, "/")) <> "\\z", "su"), negated?, dir_only?}
    end
  end

  # Three or more stars are two, as in git.
  defp regex(glob), do: glob |> String.replace(~r/\*{3,}/, "**") |> String.split("/") |> segments()

  defp segments(["**"]), do: ".*"
  defp segments(["**" | rest]), do: "(?:.*/)?" <> segments(rest)
  defp segments([name]), do: name(name)
  defp segments([name | rest]), do: name(name) <> "/" <> segments(rest)

  # Every character that is not a wildcard is escaped, so "a.txt" does not match "abtxt".
  defp name(name) do
    Regex.replace(~r{\*+|\?|[^*?]+}, name, fn
      "*" <> _stars -> "[^/]*"
      "?" -> "[^/]"
      literal -> Regex.escape(literal)
    end)
  end
end
