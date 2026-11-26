defmodule SlimServe.ThumbCacheTest do
  use ExUnit.Case, async: true

  alias SlimServe.ThumbCache

  @moduletag :tmp_dir

  # Named after the test, so it runs beside the application's own cache.
  defp start(name, dir, max_bytes) do
    start_supervised!({ThumbCache, dir: dir, max_bytes: max_bytes, name: name})
    name
  end

  # put is a cast, so this returns once the cache has handled it.
  defp put(cache, key, webp, dims \\ {1, 1}) do
    :ok = ThumbCache.put(cache, key, webp, dims)
    _ = :sys.get_state(cache)
  end

  # Shaped like a real key: 64 hex digits.
  defp key(char), do: String.duplicate(char, 64)

  defp thumb(char), do: key(char) <> ".webp"

  defp bytes(char), do: String.duplicate(char, 100)

  defp age(dir, name, seconds \\ 60), do: File.touch!(Path.join(dir, name), System.os_time(:second) - seconds)

  test "Scenario: A put past the cap deletes the oldest thumbnails", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 250)
    put(cache, key("a"), bytes("a"))
    put(cache, key("b"), bytes("b"))
    assert Enum.sort(File.ls!(dir)) == [thumb("a"), thumb("b")]

    # The second one written is now the oldest, so the order is the files' and not the puts'.
    age(dir, thumb("b"))
    put(cache, key("c"), bytes("c"))

    assert Enum.sort(File.ls!(dir)) == [thumb("a"), thumb("c")]
    assert ThumbCache.lookup(cache, key("b")) == :miss
    assert ThumbCache.lookup(cache, key("c")) == {:ok, bytes("c")}
  end

  test "Scenario: A restart keeps the cache and its cap", %{tmp_dir: dir, test: name} do
    File.write!(Path.join(dir, thumb("5")), bytes("s"))
    age(dir, thumb("5"))
    File.write!(Path.join(dir, key("e") <> ".tmp"), "partial")

    cache = start(name, dir, 150)
    assert File.ls!(dir) == [thumb("5")]

    put(cache, key("d"), bytes("d"))
    assert File.ls!(dir) == [thumb("d")]
  end

  test "Scenario: Files the cache did not write are left alone", %{tmp_dir: dir, test: name} do
    File.write!(Path.join(dir, "x.tmp"), "theirs")
    File.write!(Path.join(dir, "photo.webp"), bytes("p"))
    # The oldest file there, so an eviction that looked at it would delete it first.
    age(dir, "photo.webp", 120)

    cache = start(name, dir, 150)
    assert Enum.sort(File.ls!(dir)) == ["photo.webp", "x.tmp"]

    put(cache, key("a"), bytes("a"))
    age(dir, thumb("a"))
    put(cache, key("b"), bytes("b"))
    assert Enum.sort(File.ls!(dir)) == [thumb("b"), "photo.webp", "x.tmp"]
  end

  test "Scenario: A thumbnail is stored and read with its dimensions", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 1000)
    put(cache, key("a"), bytes("a"), {640, 480})

    assert ThumbCache.lookup(cache, key("a")) == {:ok, bytes("a")}
    assert ThumbCache.size(cache, key("a")) == {640, 480}
  end

  @tag capture_log: true
  test "Scenario: A cache directory that cannot be made leaves the server without a cache", %{tmp_dir: dir, test: name} do
    # A file where the directory's parent should be, so it fails for root too.
    File.write!(Path.join(dir, "file"), "")

    assert {:ok, cache} =
             start_supervised({ThumbCache, dir: Path.join(dir, "file/cache"), max_bytes: 1000, name: name})

    :ok = ThumbCache.put(name, key("a"), bytes("a"), {1, 1})
    # The pid, not the name: a cache that the put crashed would answer under its name again.
    _ = :sys.get_state(cache)
    assert ThumbCache.lookup(name, key("a")) == :miss
  end
end
