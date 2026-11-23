# SlimServe

SlimServe is being rebuilt as a Phoenix 1.8 application. This branch holds the base: an empty app
and the gates every later change must pass. The Go version lives on `master`.

## Run it

```sh
mise install              # Erlang, Elixir and actionlint, from .tool-versions
sudo pacman -S inotify-tools   # or apt-get: inotifywait, for the live listing
mix setup                 # deps, Tailwind and esbuild binaries, first asset build
mix phx.server            # http://localhost:4000
```

## Log in

Set `SLIMSERVE_USERNAME` and `SLIMSERVE_PASSWORD` to put every route behind a login page. Leave both
unset to serve without one; setting only one refuses to boot. In prod, login also needs a fixed
`SECRET_KEY_BASE` (`mix phx.gen.secret`): it keys the login in the session cookie, so the server
refuses to boot without it. Changing the password logs every session out.

## Edit from the browser

Set `SLIMSERVE_WRITE_PASSWORD` to let a user who logs in with it at `/.app/login` upload, rename,
move and delete files and create folders from the listing, and open `/.app/dashboard`. With viewer
login on, a write login takes the same username. An empty value counts as unset: no writes, and the
dashboard answers 404. Changing the write password ends every write session. In prod, set a fixed
`SECRET_KEY_BASE` too, or write logins end on each restart.

- `SLIMSERVE_UPLOAD_MAX_MB`: the largest upload, default 100.
- `SLIMSERVE_UPLOAD_TYPES`: a comma list of extensions, such as `jpg,png,pdf`; unset accepts any.
  An extension with no known MIME type refuses to boot.

Nothing is ever overwritten, and a deleted folder goes with everything in it, hidden files too:
there is no trash. Uploads and file moves need a file system with hard links (ext4, xfs, btrfs,
overlayfs); on exFAT or some SMB mounts they are refused. A crash mid-upload can leave a hidden
`.upload-*` file in the target folder; it is never served, and is safe to delete.

## Check it

```sh
mix precommit   # before every commit, in seconds
mix ci          # everything CI runs
```

`mix precommit` compiles with warnings as errors, checks for unused locked deps and formatting
(Styler), runs Credo and the tests. `mix ci` runs those plus `hex.audit`, Sobelow at medium
confidence, `deps.audit`, Dialyzer and the tests with a coverage floor. CI runs `mix ci`
(`.github/workflows/ci.yml`). The first Dialyzer run builds its PLTs in `priv/plts` and takes a few
minutes.

See `docs/architecture.md` for how a request flows and `AGENTS.md` for the project rules.
