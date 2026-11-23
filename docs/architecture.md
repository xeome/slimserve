# Architecture

SlimServe is one OTP application, `:slimserve`, with no database.

## Behaviour

- Directory listing: a listing shows exactly the entries the server would serve whose names are valid UTF-8, folders first and then by name.
- Listing views: a listing shows as a list with sizes and modified times, the default, or as a grid of thumbnails and kind icons; `?view=grid` or `?view=list` on a directory sets the `slimserve-view` cookie for a year and redirects to the bare path, so the choice survives a reload with no JS, and any other value is ignored.
- Live listing: a connected listing updates within a second of a file being added to or removed from its directory, under the dot-file and ignore rules.
- File download: a visible file downloads with the MIME type of its requested name, for a symlink too, whatever the Accept header, and with the same CSP and `nosniff` headers as every page; a file the server cannot read is neither listed nor served.
- Range requests: a file download answers one `bytes=a-b`, `bytes=a-` or `bytes=-n` range with a 206 and its `Content-Range`, an unsatisfiable one with a 416 and `Content-Range: bytes */<size>`, and several ranges, a malformed header or an empty file with the full 200; every download carries `Accept-Ranges: bytes`.
- Containment: a path that resolves outside the root, through a symlink or an encoded slash too, or that has a component that is not valid UTF-8 or holds a NUL byte, answers 404.
- Dot-files: a path with a component starting with ".", in the requested path or in the path a symlink resolves it to, is neither listed nor served.
- Ignore files: a path a `.slimserveignore` on its ancestry ignores, or under an ignored directory, by its requested or its resolved path, is neither listed nor served; an unreadable or non-UTF-8 ignore file ignores its whole directory; patterns are the gitignore subset `*`, `?`, `**`, a leading or inner `/` to anchor, a trailing `/` for directories and a leading `!` to negate, and the last match wins.
- On-demand thumbnail: `GET <path>?thumb=1` answers a WebP with none of the source's metadata and no larger than the configured size for a visible JPEG, PNG, GIF or WebP by both name and content, 404 for any other path, and 413 without decoding for an image over the configured input size.
- Login: with `SLIMSERVE_USERNAME` and `SLIMSERVE_PASSWORD` both set, every route answers a browser's GET with a 302 to `/.app/login?next=<path>` and anything else with a bare 401 until the user logs in, and a live socket without the login is refused at mount; a login returns to `next` only when it is a local path, else to `/`; 10 failed logins from one IP in a rolling minute make its next attempts answer 429; changing the password logs every session out; with both unset nothing changes and `/.app/login` and `/.app/logout` answer 404.
- Thumbnail cache: a repeated thumbnail comes from disk, a changed source gets a new one, and the oldest thumbnails go to keep the cache under its size; the cache deletes only files named like its own, and thumbnails still answer when the cache process is down or its directory cannot be made.

## Supervision tree

`SlimServe.Application` starts `SlimServe.Supervisor` (`:one_for_one`) with, in order:

| Child | Role |
|---|---|
| `SlimServeWeb.Telemetry` | Telemetry metrics and the periodic poller |
| `DNSCluster` | Joins nodes found by `DNS_CLUSTER_QUERY`; ignored when unset |
| `Phoenix.PubSub` (`SlimServe.PubSub`) | Message bus for LiveView and channels |
| `SlimServe.LoginLimit` | Owns the public ETS table of login attempts per client (an IPv6 one by its /64) and sweeps it every minute |
| `SlimServe.ThumbCache` | The thumbnail disk cache: the only process that writes or deletes in the cache directory |
| `SlimServe.Watcher` | Watches the root through `inotifywait` and tells open listings their directory changed; `:temporary` |
| `SlimServeWeb.Endpoint` | The Bandit HTTP server; started last, so it serves only once the rest is up |

## Request path

```
Bandit
  -> SlimServeWeb.Endpoint   Plug.Static (priv/static, at /.app), live reload in dev, RequestId,
                             Telemetry, Parsers, MethodOverride, Head, Session (signed cookie)
  -> SlimServeWeb.Router     :serve pipeline: session, the SlimServeWeb.Login gate, flash, root layout, CSRF protection,
                             secure headers with the CSP. No `accepts`: a download answers
                             any Accept header
  -> /.app/login, /.app/logout  LoginController: the form, the credential check, the logout
  -> GET /*path              BrowseController.show asks SlimServe.Root.resolve/2, the one gate:
       a directory           live_render of ListingLive inside Layouts.app, inside layouts/root
       a file                send_file with its MIME type
       a file, ?thumb=1      a WebP thumbnail from SlimServe.ThumbCache, or made by
                             SlimServe.Thumbnail (libvips) and then cached
       anything else         404, the same for a hidden path and a missing one
```

`SlimServe.Root.resolve/2` refuses a path that leaves the root (`Path.safe_relative/2` follows
every symlink), has a component starting with ".", or is ignored by a `.slimserveignore` file
(`SlimServe.Ignore`). `ListingLive` lists exactly the names `resolve/2` accepts, so the listing
and the download cannot disagree. The root is `config :slimserve, :root`, read from
`SLIMSERVE_ROOT` at boot.

A thumbnail is asked for as `<path>?thumb=1`; the listing adds `&v=<mtime>-<size>`, so an image that
was still being copied when it was first listed re-fetches once it changes. It passes the same gate, so a hidden image has no
thumbnail either. Only a file whose resolved name ends in jpg, jpeg, png, gif or webp gets one, and
only when libvips would decode it as one of those formats: libvips picks its decoder from the bytes,
so an SVG named x.png would otherwise render files it links to. Anything else answers 404. A source file over `SLIMSERVE_THUMB_MAX_FILE_SIZE_MB` (default 10)
answers 413 before libvips opens it. The thumbnail fits a square of `SLIMSERVE_THUMB_MAX_DIM` pixels
(default 250) and is never larger than its source. libvips comes precompiled inside the `vix`
package, so neither a build nor the release image installs it.

`SlimServe.ThumbCache` keeps one `<key>.webp` file per thumbnail in `SLIMSERVE_CACHE_DIR`
(default: `slimserve-thumbs` in the system temp directory). The key is a SHA-256 of the source's
path, inode, ctime, modification time, size and the thumbnail size, so a changed file misses. The directory is
the index: a hit is the file existing, and the request process reads it. libvips also runs in the
request process, and the cache process alone writes (to a `.tmp` name, then a rename) and deletes.
Past `SLIMSERVE_THUMB_CACHE_MB` (default 100) it deletes the oldest files down to 90% of the cap.
The answer's `x-cache` header says `hit` or `miss`.

## Live listing

`SlimServe.Watcher` runs `inotifywait` (the `file_system` package) on the whole root. It drops a bare
modify, so a long write wakes listings once, when the file closes. For each other event it broadcasts `{:dir_changed, rel_dir}` on `SlimServe.Root.topic(rel_dir)` for the event's
directory, and for an event on a directory also on that directory's own topic. A connected
`ListingLive` subscribes to the topic of the directory its path resolves to, so a view through a
symlink hears its target. Its first message schedules one re-list 100 ms
later, so a burst of events re-lists once. The re-list goes through `SlimServe.Root` like the first
render, so a dot-file or an ignored file cannot appear, and it sends nothing when the entries are
unchanged. When the directory no longer resolves, the view redirects to the nearest ancestor that
does.

inotify needs one watch per directory. Past `fs.inotify.max_user_watches`, `inotifywait` exits. The
Watcher then logs an error and stops, and is not restarted: files are still served, but listings no
longer update until the app restarts. Without `inotifywait` the app does not boot. A view under a
directory whose `.slimserveignore` changed updates only on its next event.

The app's own URLs live under `/.app`, a path the gate never serves: `/.app/live` is the
LiveView socket and `/.app/assets` the static files. `/dev/dashboard` (LiveDashboard) exists
only in dev, and its scope is declared before the catch-all route.

Assets: `assets/css/app.css` (Tailwind v4 plus the graphite tokens and components) and
`assets/js/app.js` are built by the `tailwind` and `esbuild` Mix tasks into `priv/static/assets`.
