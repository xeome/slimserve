defmodule SlimServe.DiskCacheTest do
  use ExUnit.Case, async: true

  alias SlimServe.DiskCache

  @moduletag :tmp_dir

  defp start(name, dir, max_bytes) do
    start_supervised!({DiskCache, name: name, dir: dir, max_bytes: max_bytes, ext: "bin"})
    name
  end

  defp key(char), do: String.duplicate(char, 64)

  defp age(dir, char, seconds), do: File.touch!(Path.join(dir, key(char) <> ".bin"), System.os_time(:second) - seconds)

  defp fill(cache, char, bytes) do
    {:ok, tmp} = DiskCache.reserve(cache, byte_size(bytes))
    File.write!(tmp, bytes)
    {:ok, path} = DiskCache.publish(cache, tmp, key(char))
    path
  end

  defp holder(cache, char) do
    test = self()

    pid =
      spawn(fn ->
        send(test, {:claimed, DiskCache.claim(cache, key(char))})
        receive(do: (:stop -> :ok))
      end)

    assert_receive {:claimed, {:ok, _path}}
    pid
  end

  test "a published file is claimed by its key, and a claim of an unknown key misses", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 1000)
    path = fill(cache, "a", "abc")

    assert DiskCache.claim(cache, key("a")) == {:ok, path}
    assert File.read!(path) == "abc"
    assert DiskCache.claim(cache, key("b")) == :miss
  end

  test "held room counts against the cap until it is released", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 100)
    assert {:ok, tmp} = DiskCache.reserve(cache, 100)
    assert DiskCache.reserve(cache, 1) == :full

    DiskCache.release(cache, tmp)
    assert {:ok, _tmp} = DiskCache.reserve(cache, 1)
  end

  test "a holder that exits gives back its room and leaves no temporary file", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 100)
    test = self()

    pid =
      spawn(fn ->
        {:ok, tmp} = DiskCache.reserve(cache, 100)
        File.write!(tmp, "partial")
        send(test, :reserved)
      end)

    ref = Process.monitor(pid)
    assert_receive :reserved
    assert_receive {:DOWN, ^ref, :process, ^pid, :normal}
    _ = :sys.get_state(cache)

    assert File.ls!(dir) == []
    assert {:ok, _tmp} = DiskCache.reserve(cache, 100)
  end

  test "eviction takes the least recently touched file, and never a held one", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 250)
    for char <- ~w(a b), do: DiskCache.release(cache, fill(cache, char, String.duplicate(char, 100)))
    age(dir, "a", 120)
    age(dir, "b", 60)

    # A claim touches a: b is now the least recently used.
    {:ok, a} = DiskCache.claim(cache, key("a"))
    DiskCache.release(cache, a)
    DiskCache.release(cache, fill(cache, "c", String.duplicate("c", 100)))
    assert Enum.sort(File.ls!(dir)) == [key("a") <> ".bin", key("c") <> ".bin"]

    # Held through a send, a stays even though it is now the oldest.
    age(dir, "a", 300)
    sender = holder(cache, "a")
    DiskCache.release(cache, fill(cache, "d", String.duplicate("d", 100)))
    assert Enum.sort(File.ls!(dir)) == [key("a") <> ".bin", key("d") <> ".bin"]
    send(sender, :stop)
  end

  test "room the cap can never hold is refused without evicting anything", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 100)
    DiskCache.release(cache, fill(cache, "a", "abc"))

    assert DiskCache.reserve(cache, 101) == :full
    assert File.ls!(dir) == [key("a") <> ".bin"]
  end

  test "a cache directory removed from outside is made again when room is reserved", %{tmp_dir: dir, test: name} do
    sub = Path.join(dir, "cache")
    cache = start(name, sub, 100)
    DiskCache.release(cache, fill(cache, "a", String.duplicate("a", 90)))
    File.rm_rf!(sub)

    assert {:ok, tmp} = DiskCache.reserve(cache, 50)
    assert File.write(tmp, "b") == :ok
  end

  test "an object published twice by two misses is counted once", %{tmp_dir: dir, test: name} do
    cache = start(name, dir, 100)
    {:ok, first} = DiskCache.reserve(cache, 40)
    {:ok, second} = DiskCache.reserve(cache, 40)
    for tmp <- [first, second], do: File.write!(tmp, String.duplicate("a", 40))
    for tmp <- [first, second], do: DiskCache.release(cache, elem(DiskCache.publish(cache, tmp, key("a")), 1))

    assert {:ok, _tmp} = DiskCache.reserve(cache, 60)
    assert {:ok, _path} = DiskCache.claim(cache, key("a"))
  end
end
