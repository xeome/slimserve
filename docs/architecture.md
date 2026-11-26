# Architecture

SlimServe is one OTP application, `:slimserve`, with no database.

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
The answer's `x-cache` header says `hit` or `miss`. Beside each thumbnail it stores the source's
shown pixel size (EXIF rotation applied) as a `WxH` file under a key derived from the thumbnail's,
which the inspector reads; a source that never had a thumbnail made shows no size.

`SlimServe.Kind` sorts a name into image, video, audio, document, archive, code or other by its
extension, for the Kind column, the card and the inspector; it gates nothing.

## Live listing

`SlimServe.Watcher` runs `inotifywait` (the `file_system` package) on the whole root. It drops a bare
modify, so a long write wakes listings once, when the file closes. For each other event it broadcasts `{:dir_changed, rel_dir}` on `SlimServe.Root.topic(rel_dir)` for the event's
directory, and for an event on a directory also on that directory's own topic. A connected
`ListingLive` subscribes to the topic of the directory its path resolves to, so a view through a
symlink hears its target. Its first message schedules one re-list 100 ms
later, so a burst of events re-lists once. The re-list goes through `SlimServe.Root` like the first
render, so a dot-file or an ignored file cannot appear, and it sends nothing when the entries are
unchanged: the sidebar tree, which lists each ancestor and counts each folder shown, is rebuilt
only when they changed. When the directory no longer resolves, the view redirects to the nearest ancestor that
does. A folder the store could not list keeps its rows and is listed again, after 200 ms and then
twice as long each time up to 30 s, until it lists.

inotify needs one watch per directory. Past `fs.inotify.max_user_watches`, `inotifywait` exits. The
Watcher then logs an error and stops, and is not restarted: files are still served, but listings no
longer update until the app restarts. Without `inotifywait` the app does not boot. A view under a
directory whose `.slimserveignore` changed updates only on its next event.

The app's own URLs live under `/.app`, a path the gate never serves: `/.app/live` is the
LiveView socket, `/.app/assets` the static files and `/.app/dashboard` LiveDashboard, whose scope is
declared before the catch-all route. Routes compile before runtime config is read, so the dashboard
exists in every env and `plug SlimServeWeb.Login, :write` answers 404 for it with no write password.

## Writes

Every write goes through `SlimServe.Root.target/4`, the write side of the gate: the parent must
resolve to a directory, the name must be one plain name, and the name must not be ignored at its
requested or its resolved path (`dir?: true` for a folder to be made, so a dir-only pattern
applies). It answers the name's own path, so a link is acted on as a link. `ListingLive` adds that
a new name has nothing at it, not even a dangling link, and that an existing one resolves, so it is
visible, that a link is renamed only within its own folder, and, with `SLIMSERVE_UPLOAD_TYPES`
set, that a file's new name has a listed extension, whatever type the browser claimed. Every event
`ListingLive` handles but `select`, `deselect` and `filter` is a write, so an `attach_hook` on
`handle_event` halts every other event, an unknown one too, unless
the session's `"write"` digest, an HMAC of the write password keyed with the server secret,
still matches at that event.

An upload is copied to a `.upload-<random>` name in the target folder, hard-linked to its name and
the temporary name removed: a dot name is never served, and a hard link refuses an existing name,
so an upload never overwrites. A file moves the same way, by a hard link and a remove; a folder moves
by `File.rename/2`. So the root must support hard links: on exFAT and some SMB mounts uploads and
file moves are refused. A folder delete uses `File.rm_rf/1`, which removes links inside the tree
without following them. No write broadcasts: `SlimServe.Watcher` sees each change and tells the
open listings, the writer's too.

Assets: `assets/css/app.css` (Tailwind v4 plus the graphite tokens and components) and
`assets/js/app.js` are built by the `tailwind` and `esbuild` Mix tasks into `priv/static/assets`.
