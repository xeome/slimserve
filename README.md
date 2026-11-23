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
