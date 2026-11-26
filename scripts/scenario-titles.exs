root = Path.expand("..", __DIR__)

titles = fn glob, regex ->
  for path <- Path.wildcard(Path.join(root, glob)),
      [_, title] <- Regex.scan(regex, File.read!(path)),
      into: MapSet.new(),
      do: String.trim(title)
end

specs = titles.("openspec/specs/**/*.md", ~r/^#### Scenario:(.*)$/m)
tests = titles.("test/**/*.exs", ~r/^\s*(?:test|property)\(?\s*"Scenario:([^"]*)"/m)

if Enum.empty?(specs) or Enum.empty?(tests) do
  IO.puts(:stderr, "No scenario titles under #{root}: #{MapSet.size(specs)} in specs, #{MapSet.size(tests)} in tests.")
  System.halt(1)
end

case tests |> MapSet.difference(specs) |> Enum.sort() do
  [] ->
    :ok

  stray ->
    IO.puts(:stderr, "Test scenarios with no matching spec heading:")
    Enum.each(stray, &IO.puts(:stderr, "  " <> &1))
    System.halt(1)
end
