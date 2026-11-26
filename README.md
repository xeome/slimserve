# SlimServe

SlimServe is being rebuilt as a Phoenix 1.8 application. This branch holds the base: an empty app
and the gates every later change must pass. The Go version lives on `master`.

## Run it

```sh
mise install              # Erlang, Elixir, actionlint and bun, from .tool-versions
sudo pacman -S inotify-tools   # or apt-get: inotifywait, for the live listing
mix setup                 # deps, Tailwind and esbuild binaries, first asset build
mix phx.server            # http://localhost:4000
```

Production releases serve plain HTTP by default. For internet-facing deployments, terminate TLS
at a trusted proxy and enable HTTPS enforcement. Behind any TLS proxy, even on a LAN, set
`SLIMSERVE_HTTPS=true` and set `PHX_HOST` to the hostname clients use.

## Deploy with Helm

Chart 2.0.0 uses the Elixir image. For a fresh install, create the namespace and choose
an available Elixir image. For a Go upgrade, keep the release name, release namespace
and PVC size, then provide new values explicitly, including credentials for any enabled
Go viewer or dashboard login. See the [Helm guide](manifests/helm/README.md) for both commands.
For production, terminate TLS outside the chart; it does not create an Ingress.

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

## Serve an S3 bucket

Set `SLIMSERVE_STORAGE=s3` to serve a private bucket of any S3-compatible store, MinIO included,
instead of `SLIMSERVE_ROOT`:

- `SLIMSERVE_S3_BUCKET`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` (required), `AWS_REGION`
  (default `us-east-1`), `AWS_SESSION_TOKEN`, and `AWS_ENDPOINT_URL_S3` for a store other than AWS.
  Requests are path-style, so the endpoint names the store, not the bucket.
- AWS itself is untested. Set `AWS_ENDPOINT_URL_S3` to the bucket region's endpoint there too
  (`https://s3.<region>.amazonaws.com`): without it, requests go to the global host, and a bucket
  that redirects to its regional host answers every request with a 502.
- `SLIMSERVE_DOWNLOAD_CACHE_DIR` and `SLIMSERVE_DOWNLOAD_CACHE_MB` (default 1024): files up to
  100 MB are kept here once fetched, so a repeat download, ranged or not, reads no bytes from S3.
  Larger files, and misses the cache has no room for, stream through.

SlimServe must be the bucket's only writer. A change made through SlimServe shows at once on every
node. A change made in the bucket directly shows on any page loaded after the cached listing
expires, at most 10 minutes after it was filled. A page already open shows it only when a write
through SlimServe wakes it or the page reloads.
`.slimserveignore` files and hidden names work as on disk; a key whose name is not valid UTF-8, or
holds a NUL, is not listed.

With `SLIMSERVE_WRITE_PASSWORD` set, the server first writes and removes a few objects under the
bucket's hidden `.slimserve-probe/` prefix, to learn which conditional writes the store enforces.
Uploads and new folders are refused, with a warning at boot, unless it refuses to overwrite an
existing key. Where it ignores a stale `If-Match` (MinIO does, on DELETE), move and delete check
the object themselves just before acting, and a change made in that instant can be lost. A move is
a copy then a delete: files over 5 GiB copy in 5 GiB parts, and a move or delete that fails
part-way says so and leaves what it did.

## Check it

```sh
mix precommit   # before every commit, in seconds
mix ci          # everything CI runs
```

`mix precommit` compiles with warnings as errors, checks for unused locked deps and formatting
(Styler), runs Credo and the tests. `mix ci` runs those plus `hex.audit`, Sobelow at medium
confidence, `deps.audit`, Dialyzer and the tests with a coverage floor. CI runs `mix ci`, then the
S3 suite against a throwaway MinIO (`scripts/test-s3.sh`, needs Docker) and the chart suite
(`mix test --only helm`, needs `helm`) (`.github/workflows/ci.yml`). The first Dialyzer run builds its PLTs in `priv/plts` and takes a few
minutes.

See `docs/architecture.md` for how a request flows and `AGENTS.md` for the project rules.
