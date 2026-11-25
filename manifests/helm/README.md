# SlimServe Helm chart

The chart runs one Elixir replica and briefly stops service during an upgrade (`Recreate`). It mounts `/data` from `<release>-data-pvc`, using `subpath`, with `fsGroup: 1001`. The storage backend must support that group or already let UID/GID 1001 read and (for browser edits) write the files. Set `persistence.enabled: false` only for a disposable install: files in `emptyDir` vanish with the pod.

The chart does not inherit the Go chart's 512 MiB memory limit: it can prevent the Elixir VM from starting. Size any `resources.limits.main.memory` override for the Elixir workload on your nodes.

`host` is required; set it to the public hostname for Phoenix-generated URLs.

## Serve an S3 bucket

Set `storage.backend: s3`, `s3.bucket`, `s3.accessKeyId` and `s3.secretAccessKey`, plus
`s3.endpoint` (the store's URL, path-style) for anything but AWS and `s3.region` if not
`us-east-1`. AWS itself is untested: set `s3.endpoint` to the bucket region's endpoint
(`https://s3.<region>.amazonaws.com`) there too, since a bucket that redirects to its regional
host answers every request with a 502. The pod then mounts no `/data`. An existing data claim stays, unmounted, so a switch
back to local finds its files, and the chart still refuses to drop it. Each pod keeps fetched files
up to 100 MB in an `emptyDir` capped at `s3.downloadCacheMB` (default 1024), whose size limit
adds 10% and 16 MiB of headroom for filesystem blocks. The root README lists what
the store must support.

Over S3, `replicas` can be more than 1. That needs `cluster.cookie`, a shared secret the pods join
their Erlang cluster with, and a fixed `auth.secretKeyBase`, so a page served by one pod connects to
any other. The pods find each other through the headless `<release>-nodes` Service, which carries
no HTTP; the HTTP Service is unchanged. The cookie and the S3 keys go into `<release>-runtime`
with the other credentials.

## Fresh install

The chart's default image is `ghcr.io/xeome/slimserve:elixir-0.1.0`. That tag is not published yet; build the repository `Dockerfile`, make the image available to the cluster, and set `image.main.repository` and `image.main.tag` in your values file. For an image loaded into a local cluster, use `image.main.pullPolicy: Never`.

```sh
kubectl create namespace slimserve  # if it does not exist
helm install files ./manifests/helm -n slimserve -f elixir-values.yaml --wait
```

The workload normally uses the Helm release namespace. For compatibility, a release in `default` puts its workloads in `slimserve` instead; create `slimserve` before installing or upgrading. `port` controls the Elixir listener and probes; `service.type` and `service.ports` set exposure, with Service targets always pointing to that listener.

The chart enables HTTPS enforcement with `SLIMSERVE_HTTPS=true`; it does not create an Ingress or terminate TLS. Keep the HTTP backend private behind a trusted TLS terminator that preserves the public `Host` and replaces any client-supplied `X-Forwarded-Proto` with the original request scheme. The HTTP readiness check sends `Host: localhost` to `/` to bypass the redirect; liveness checks only the named `http` TCP port, so a slow directory listing does not cause a restart.

## Upgrade an existing Go release

1. Back up the claim, and inspect the old chart values (`helm get values <release> -n <release-namespace> -a`). Keep the release name, Helm namespace, and `resources.requests.main.storage` unchanged. Do not uninstall the Go chart first. Keep persistence on. You can carry over `service.type`, `service.ports`, image pull policy, resource limits, `nodeSelector`, `affinity`, and `tolerations`; choose an **Elixir** image, not the Go tag.
2. Prepare a new values file. Replace `main.auth` (including legacy Secrets or Go environment settings) with `auth.username`, `auth.password`, and a fixed `auth.secretKeyBase` (`mix phx.gen.secret`). `auth.enabled: true` explicitly requires all three. To replace an enabled Go dashboard, set `auth.writePassword` to a **different** password from the viewer password. With viewer login enabled, both use the same username; the write password grants uploads and other mutations. Leave it unset for a read-only install without a legacy dashboard. If only the Go dashboard was protected, set the write password without viewer credentials. A fixed key keeps write sessions valid across restarts. Do not reuse the Go credentials implicitly: supply the required replacements even if they were stored in an external Secret. `main.options`, `main.environments`, and `main.ports` are not applied to Elixir. Map `SLIMSERVE_MAX_UPLOAD_SIZE_MB` to `upload.maxMB` and `SLIMSERVE_PORT` to `port` if customized; the served root is always `/data`.
3. Use the **same Helm release namespace** (even `default`) for the upgrade. Preview with `--dry-run=server --hide-secret`, not with an offline render: the chart checks the live Go Deployment for auth flags when old values are reset. Use a Helm identity with read access to the workload namespace. Do not remove the old workload before the check.

```sh
helm upgrade <release> ./manifests/helm -n <release-namespace> \
  --reset-values -f elixir-values.yaml --wait
```

The chart puts only the supplied passwords and signing key into the `<release>-runtime` Kubernetes Secret. Keep the values file private and restrict access to Kubernetes Secrets and Helm release records, which also store the submitted values. A fresh install without viewer credentials is public; a viewer who signs in at `/.app/login` does not gain write access unless they use the separate write password.

An upgrade with credentials rolls the pod using the Helm release revision, not a password-derived checksum, so the pod reads the updated Secret. To revoke a write password while keeping viewer access, remove `auth.writePassword` from the new values and upgrade with `--reset-values`.
