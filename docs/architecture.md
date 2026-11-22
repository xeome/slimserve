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
  -> SlimServeWeb.Endpoint   Plug.Static (priv/static), live reload in dev, RequestId, Telemetry,
                             Parsers, MethodOverride, Head, Session (signed cookie)
  -> SlimServeWeb.Router     :browser pipeline: accepts html, session, flash, root layout,
                             CSRF protection, secure headers with the CSP
  -> PageController.home     renders PageHTML.home inside Layouts.app, inside layouts/root
```

`/live` is the LiveView socket. `/dev/dashboard` (LiveDashboard) exists only in dev.

Assets: `assets/css/app.css` (Tailwind v4 plus the graphite tokens and components) and
`assets/js/app.js` are built by the `tailwind` and `esbuild` Mix tasks into `priv/static/assets`.
