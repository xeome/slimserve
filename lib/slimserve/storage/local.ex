defmodule SlimServe.Storage.Local do
  @moduledoc """
  A local folder's changes reach open pages through `SlimServe.Watcher`, so a write here broadcasts nothing itself.
  """

  @behaviour SlimServe.Storage

  alias SlimServe.Ignore
  alias SlimServe.Root
  alias SlimServe.Storage
  alias SlimServe.Thumbnail

  @impl true
  def resolve(segments), do: Root.resolve(root(), segments)

  # The Watcher names the directory a symlink leads to, not the link.
  @impl true
  def topics(segments) do
    case resolve(segments) do
      {:dir, dir} -> [Root.topic(Path.relative_to(dir, root()))]
      _ -> []
    end
  end

  # Exactly the names Root.resolve/2 would serve: the listing has no filter of its own.
  # File.ls/1 skips names that are not valid UTF-8 (spec: Directory listing).
  @impl true
  def list(segments, _opts) do
    thumb_max_file_bytes = Application.fetch_env!(:slimserve, :thumb_max_file_bytes)

    with {:dir, dir} <- resolve(segments),
         {:ok, names} <- File.ls(dir) do
      # ponytail: resolve/2 re-reads the ancestors' ignore files for every name.
      # Load them once per listing if big folders get slow.
      entries =
        for name <- names,
            {kind, abs} <- [resolve(segments ++ [name])],
            {:ok, stat} <- [File.stat(abs, time: :posix)] do
          %{
            name: name,
            dir?: kind == :dir,
            size: stat.size,
            mtime: stat.mtime,
            # The same rule as the thumbnail route, so the listing asks only for thumbnails it can get.
            thumb:
              if(kind == :file and Thumbnail.image?(abs) and stat.size <= thumb_max_file_bytes,
                do: "#{stat.mtime}-#{stat.size}"
              )
          }
        end

      {:ok, Enum.sort_by(entries, &{not &1.dir?, &1.name})}
    else
      _ -> :error
    end
  end

  # Written under a dot name first, so a half-written file is never served. A hard link refuses an
  # existing name, so an upload never overwrites one, with no gap between a check and the write.
  @impl true
  def upload(segments, name, tmp) do
    with {:ok, dest} <- fresh(segments, name) do
      temp = Path.join(Path.dirname(dest), ".upload-" <> Base.url_encode64(:crypto.strong_rand_bytes(12)))

      try do
        result(with :ok <- File.cp(tmp, temp), do: File.ln(temp, dest))
      after
        File.rm(temp)
      end
    end
  end

  @impl true
  def mkdir(segments, name), do: result(with({:ok, abs} <- fresh(segments, name, dir?: true), do: File.mkdir(abs)))

  @impl true
  def move(segments, from, parent, name) do
    result(
      with {:ok, src, followed_kind, lstat_type} <- source(segments, from),
           true <- followed_kind == :dir or Storage.upload_type?(name),
           {:ok, dest} <- fresh(parent, name, dir?: followed_kind == :dir),
           # A relative link's target would mean another path in another folder.
           true <- lstat_type != :symlink or Path.dirname(dest) == Path.dirname(src) do
        rename(src, dest, lstat_type, segments ++ [from])
      end
    )
  end

  # File.lstat, not resolve, picks the call, so a link is removed as a link, a link to a folder included.
  @impl true
  def delete(segments, name) do
    case source(segments, name) do
      {:ok, abs, _followed_kind, :directory} ->
        case {File.ls(abs), confirm(name, abs)} do
          {{:ok, []}, _confirm} -> result(File.rmdir(abs))
          {{:ok, _names}, %{} = confirm} -> {:confirm, confirm}
          _error -> :error
        end

      {:ok, abs, _followed_kind, _lstat_type} ->
        result(File.rm(abs))

      :error ->
        :error
    end
  end

  # File.rm_rf/1 removes a link inside the tree without following it.
  @impl true
  def confirm_delete(segments, name, asked) do
    with {:ok, abs, _followed_kind, :directory} <- source(segments, name),
         now = confirm(name, abs),
         ^asked <- now,
         {:ok, _removed} <- File.rm_rf(abs) do
      :ok
    else
      %{} = now -> {:confirm, now}
      _error -> :error
    end
  end

  defp result(:ok), do: :ok
  defp result(_refused), do: :error

  defp confirm(name, abs) do
    with {:ok, %File.Stat{type: :directory, inode: inode}} <- File.lstat(abs),
         do: %{name: name, count: count_files(abs), inode: inode}
  end

  # A file moves by a hard link, which refuses an existing name; File.rename/2 has no such flag.
  defp rename(src, dest, :directory, requested) do
    if hidden_inside?(src, requested), do: :error, else: File.rename(src, dest)
  end

  defp rename(src, dest, _type, _requested) do
    with :ok <- File.ln(src, dest), do: File.rm(src)
  end

  # Ignore patterns are relative to their ignore file, so a moved folder could reveal an entry one
  # hides at its current path. Dot-names stay hidden anywhere, so they are not looked at.
  # src is never the root itself, whose relative path would be ".": source/2 takes one plain name.
  defp hidden_inside?(src, requested) do
    root = root()
    resolved = Path.split(Path.relative_to(src, root))

    Enum.any?(visible_names(src), fn rel ->
      Enum.any?(Enum.uniq([requested ++ rel, resolved ++ rel]), &Ignore.ignored?(root, &1))
    end)
  end

  defp visible_names(dir) do
    for name <- ls(dir), not String.starts_with?(name, "."), path = Path.join(dir, name), rest <- [[] | below(path)] do
      [name | rest]
    end
  end

  defp below(path), do: if(directory?(path), do: visible_names(path), else: [])

  # Files in subfolders count too, because the delete takes them all.
  defp count_files(dir) do
    for name <- ls(dir), path = Path.join(dir, name), reduce: 0 do
      n -> if directory?(path), do: n + count_files(path), else: n + 1
    end
  end

  defp directory?(path), do: match?({:ok, %File.Stat{type: :directory}}, File.lstat(path))

  defp ls(dir) do
    case File.ls(dir) do
      {:ok, names} -> names
      {:error, _reason} -> []
    end
  end

  # A new name: it passes the gate and nothing, not even a dangling link, is there yet.
  defp fresh(segments, name, opts \\ []) do
    with {:ok, abs} <- Root.target(root(), segments, name, opts),
         {:error, :enoent} <- File.lstat(abs) do
      {:ok, abs}
    else
      _ -> :error
    end
  end

  defp source(segments, name) do
    with {:ok, abs} <- Root.target(root(), segments, name),
         {followed_kind, _path} when followed_kind in [:dir, :file] <- resolve(segments ++ [name]),
         {:ok, %File.Stat{type: lstat_type}} <- File.lstat(abs) do
      {:ok, abs, followed_kind, lstat_type}
    else
      _ -> :error
    end
  end

  defp root, do: Application.fetch_env!(:slimserve, :root)
end
