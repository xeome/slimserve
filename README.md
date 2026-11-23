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

## Check it

```sh
mix precommit
```

It compiles with warnings as errors, checks formatting (Styler), runs Credo strict, Sobelow,
`deps.audit`, `hex.audit`, Dialyzer, the tests with a coverage floor, and validates the OpenSpec
specs. CI runs the same alias (`.github/workflows/ci.yml`). The first Dialyzer run builds its PLTs
in `priv/plts` and takes a few minutes.

See `docs/architecture.md` for how a request flows and `AGENTS.md` for the project rules.
