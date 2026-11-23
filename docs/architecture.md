# Architecture

SlimServe is one OTP application, `:slimserve`, with no database.

## Supervision tree

`SlimServe.Application` starts `SlimServe.Supervisor` (`:one_for_one`) with, in order:

| Child | Role |
|---|---|
| `SlimServeWeb.Telemetry` | Telemetry metrics and the periodic poller |
| `DNSCluster` | Joins nodes found by `DNS_CLUSTER_QUERY`; ignored when unset |
| `Phoenix.PubSub` (`SlimServe.PubSub`) | Message bus for LiveView and channels |
| `SlimServeWeb.Endpoint` | The Bandit HTTP server; started last, so it serves only once the rest is up |

## Request path

```
Bandit
  -> SlimServeWeb.Endpoint   Plug.Static (priv/static, at /.app), live reload in dev, RequestId,
                             Telemetry, Parsers, MethodOverride, Head, Session (signed cookie)
  -> SlimServeWeb.Router     :serve pipeline: session, flash, root layout, CSRF protection,
                             secure headers with the CSP. No `accepts`: a download answers
                             any Accept header
  -> GET /*path              BrowseController.show asks SlimServe.Root.resolve/2, the one gate:
       a directory           live_render of ListingLive inside Layouts.app, inside layouts/root
       a file                send_file with its MIME type
       anything else         404, the same for a hidden path and a missing one
```

`SlimServe.Root.resolve/2` refuses a path that leaves the root (`Path.safe_relative/2` follows
every symlink), has a component starting with ".", or is ignored by a `.slimserveignore` file
(`SlimServe.Ignore`). `ListingLive` lists exactly the names `resolve/2` accepts, so the listing
and the download cannot disagree. The root is `config :slimserve, :root`, read from
`SLIMSERVE_ROOT` at boot. The requirements are in `openspec/specs/file-serving/spec.md`.

The app's own URLs live under `/.app`, a path the gate never serves: `/.app/live` is the
LiveView socket and `/.app/assets` the static files. `/dev/dashboard` (LiveDashboard) exists
only in dev, and its scope is declared before the catch-all route.

Assets: `assets/css/app.css` (Tailwind v4 plus the graphite tokens and components) and
`assets/js/app.js` are built by the `tailwind` and `esbuild` Mix tasks into `priv/static/assets`.
