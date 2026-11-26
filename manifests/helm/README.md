# SlimServe Helm chart

The chart runs SlimServe in Kubernetes. It serves a persistent volume by default, or an S3 bucket.

## Install

The chart's default image is `ghcr.io/xeome/slimserve:elixir-0.1.0`. That tag is not published
yet. So build the repository's `Dockerfile`, make the image available to the cluster, and set
`image.main.repository` and `image.main.tag` in your values file. For an image loaded straight into
a local cluster, also set `image.main.pullPolicy: Never`.

`host` is required: set it to the public hostname, for the URLs the app builds.

```sh
kubectl create namespace slimserve  # if it does not exist
helm install files ./manifests/helm -n slimserve -f values.yaml --wait
```

The workload goes in the Helm release namespace. A release in `default` is the exception: its
workload goes in `slimserve`, so create that namespace before you install or upgrade.

## Storage

The pod mounts `/data` from the `subpath` folder of `<release>-data-pvc`, with `fsGroup: 1001`.
The volume must support that group, or already let UID and GID 1001 read the files, and write them
if you turn on browser edits. `persistence.enabled: false` puts the files in an `emptyDir`, which vanishes
with the pod, so use it only for a throwaway install.

Local storage runs one replica, because the claim is `ReadWriteOnce`. Pods are replaced during an
upgrade (`Recreate`), so an upgrade briefly stops service.

The chart sets no memory limit. If you set `resources.limits.main.memory`, size it for the Erlang
VM on your nodes: 512 MiB, the old Go chart's limit, can keep it from starting.

## Network and TLS

`port` sets the port the app listens on, and the probes use it. `service.type` and `service.ports`
set how it is exposed. The Service always targets the app's port.

The chart sets `SLIMSERVE_HTTPS=true`, so the app redirects plain HTTP to HTTPS. It does not create
an Ingress or terminate TLS. Keep the HTTP backend private, behind a TLS terminator you trust. That
terminator must pass the public `Host` through and replace any `X-Forwarded-Proto` the client sent
with the scheme of the original request.

## Login and secrets

Set `auth.username`, `auth.password` and a fixed `auth.secretKeyBase` (`openssl rand -base64 48`)
to put the site behind a login. `auth.enabled: true` makes all three required. `auth.writePassword`
turns on editing from the browser, and must differ from `auth.password`. Without a viewer login,
anyone can browse, and only the write password can edit.

The chart puts the passwords and `auth.secretKeyBase`, and the S3 keys and cluster cookie below, in
the `<release>-runtime` Secret. `auth.username` is a plain environment value in the Deployment.
Keep your values file private. Restrict who can read Secrets and Helm release records,
because the release records hold the submitted values too.

With credentials set, every upgrade restarts the pod, so it reads the updated Secret. To revoke a write
password and keep viewer access, remove `auth.writePassword` from your values and upgrade with
`--reset-values`.

## Serving an S3 bucket

Set `storage.backend: s3`, `s3.bucket`, `s3.accessKeyId` and `s3.secretAccessKey`. For anything
but AWS, set `s3.endpoint` to the store's URL (requests are path-style). Set `s3.region` if it is
not `us-east-1`. AWS itself is untested. If you try it, set `s3.endpoint` to the bucket region's
endpoint (`https://s3.<region>.amazonaws.com`), because a bucket that redirects to its regional
host answers every request with a 502. The [root README](../../README.md#serving-an-s3-bucket) lists
what the store must support.

The pod then mounts no `/data`. An existing data claim stays, unmounted, so switching back to local
finds its files, and the chart refuses to drop it.

Each pod caches fetched files up to 100 MB in an `emptyDir` capped at `s3.downloadCacheMB` (default
1024).

Over S3, `replicas` can be more than 1. That needs `cluster.cookie`, a shared secret the pods join
their Erlang cluster with, and a fixed `auth.secretKeyBase`, so a page served by one pod can
reconnect to any other. The pods find each other through the headless `<release>-nodes` Service,
which carries no HTTP. The HTTP Service is unchanged.

## Upgrading from the Go chart

Chart 2.0.0 runs the Elixir image. You can upgrade a Go release in place and keep its data.

1. Back up the claim. Read the old values with `helm get values <release> -n <namespace> -a`. Keep
   the release name, the Helm namespace and `resources.requests.main.storage` unchanged, and keep
   persistence on. Do not uninstall the Go chart first.
2. Write a new values file. You can carry over `service.type`, `service.ports`, the image pull
   policy, resource limits, `nodeSelector`, `affinity` and `tolerations`. Choose an Elixir image,
   not the Go tag.
3. Replace `main.auth`, and any Secret or Go environment setting it used, with `auth.username`,
   `auth.password` and `auth.secretKeyBase`. If the Go dashboard had a login, set
   `auth.writePassword` to a password different from the viewer one. With a viewer login, both use
   the same username. If only the Go dashboard was protected, set the write password alone. Supply
   every credential again, even ones the Go chart kept in an external Secret: none carries over.
4. `main.options`, `main.environments` and `main.ports` do not apply. Map
   `SLIMSERVE_MAX_UPLOAD_SIZE_MB` to `upload.maxMB` and `SLIMSERVE_PORT` to `port` if you changed
   them. The served folder is always `/data`.
5. Upgrade in the same Helm namespace as the Go release, even `default`. Preview with
   `--dry-run=server --hide-secret`, not an offline render, because the chart reads the live Go
   Deployment to check its auth flags. That needs a Helm identity that can read the workload
   namespace. Do not remove the old workload before this check.

```sh
helm upgrade <release> ./manifests/helm -n <namespace> \
  --reset-values -f values.yaml --wait
```
