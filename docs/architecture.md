# Architecture

SlimServe is one OTP application, `:slimserve`, with no database.

## Behaviour

- Directory listing: a listing shows exactly the entries the server would serve whose names are valid UTF-8, folders first and then by name.
- Listing views: a listing shows as a list with sizes and modified times, the default, or as a grid of thumbnails and kind icons; `?view=grid` or `?view=list` on a directory sets the `slimserve-view` cookie for a year and redirects to the bare path, so the choice survives a reload with no JS, and any other value is ignored.
- Listing page: a sidebar holds the folder tree (the root's folders and each open ancestor's, each with its entry count), an "In this folder" card that counts and sizes the files by kind, and the session label with log in or log out; it folds to an icon rail by its button or `[`, and the `slimserve-rail` cookie keeps that across reloads (folded by default at phone width, where the open sidebar is a drawer). The panel's header holds the crumbs, the count and total size, a name filter (`/` focuses it), and the List/Grid switch. The list has Name, Kind, Size and Modified columns, the time relative with the UTC time on hover, and a ⋯ menu per row.
- Inspector: clicking a row selects it and opens an inspector with a preview (the thumbnail, an SVG, a video or audio player, or the kind's icon), the kind with the pixel size once the thumbnail has been made, size, UTC modified time and folder, and Download (Open for a folder) and Copy link. Selecting, deselecting and filtering are read events any viewer may send; they survive a re-list, and a selected entry that disappears closes the inspector.
- Live listing: a connected listing updates within a second of a file being added to or removed from its directory, under the dot-file and ignore rules.
- File download: a visible file downloads with the MIME type of its requested name, for a symlink too, whatever the Accept header, and with the same CSP and `nosniff` headers as every page; a file the server cannot read is neither listed nor served.
- Range requests: a file download answers one `bytes=a-b`, `bytes=a-` or `bytes=-n` range with a 206 and its `Content-Range`, an unsatisfiable one with a 416 and `Content-Range: bytes */<size>`, and several ranges, a malformed header or an empty file with the full 200; every download carries `Accept-Ranges: bytes`.
- Containment: a path that resolves outside the root, through a symlink or an encoded slash too, or that has a component that is not valid UTF-8 or holds a NUL byte, answers 404.
- Dot-files: a path with a component starting with ".", in the requested path or in the path a symlink resolves it to, is neither listed nor served.
- Ignore files: a path a `.slimserveignore` on its ancestry ignores, or under an ignored directory, by its requested or its resolved path, is neither listed nor served; an unreadable or non-UTF-8 ignore file ignores its whole directory; patterns are the gitignore subset `*`, `?`, `**`, a leading or inner `/` to anchor, a trailing `/` for directories and a leading `!` to negate, and the last match wins.
- On-demand thumbnail: `GET <path>?thumb=1` answers a WebP with none of the source's metadata and no larger than the configured size for a visible JPEG, PNG, GIF or WebP by both name and content, 404 for any other path, and 413 without decoding for an image over the configured input size.
- Login: with `SLIMSERVE_USERNAME` and `SLIMSERVE_PASSWORD` both set, every route answers a browser's GET with a 302 to `/.app/login?next=<path>` and anything else with a bare 401 until the user logs in, and a live socket without the login is refused at mount; a login returns to `next` only when it is a local path, else to `/`; 10 failed logins from one IP in a rolling minute make its next attempts answer 429; changing the password logs every session out; with both unset nothing changes and `/.app/login` and `/.app/logout` answer 404.
- Write login: with `SLIMSERVE_WRITE_PASSWORD` set, `/.app/login` also takes the write password, under the same rate limit; with viewer login on the username must match too, and with it off the form asks only for the password and every listing's header links to it; a write login holds until logout, and changing or removing the write password ends it, on open pages at their next write too; logout closes that browser's open pages, which reconnect without write rights.
- Writes: with write rights, a listing uploads files (at most `SLIMSERVE_UPLOAD_MAX_MB`, default 100, and only the `SLIMSERVE_UPLOAD_TYPES` extensions when set), creates a folder, renames or moves an entry to a path from the root (the inspector's name field renames within the folder, F2 focuses it; Move to… or M opens a folder picker; a row dropped on a sidebar folder moves there; Delete asks to delete the selection), and deletes a file, a link (never its target) or a folder with everything in it after a confirm bar that counts its files, which asks again when the folder or its count changed before the confirm; other open pages update through the live listing; without write rights every write event is refused.
- Write refusals: a write is refused when it names a dot-file, a name with a "/", an empty or non-UTF-8 name, an ignored name by its requested or resolved path, a path outside the root or under a hidden or ignored folder, or an existing name, so nothing is ever overwritten; a folder whose subtree holds an entry an ignore pattern hides at its current path does not move, and a folder does not move into itself.
- Dashboard: `/.app/dashboard` (LiveDashboard) answers 404 with no write password, a write session's browser only, else a 302 to the login page or a 401, and its live socket is refused at mount without write rights.
- Thumbnail cache: a repeated thumbnail comes from disk, with the source's pixel size beside it for the inspector, a changed source gets a new one, and the oldest thumbnails go to keep the cache under its size; the cache deletes only files named like its own, and thumbnails still answer when the cache process is down or its directory cannot be made.

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
