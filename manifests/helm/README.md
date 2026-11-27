# SlimServe Helm chart

The chart runs SlimServe in Kubernetes. It serves a persistent volume by default, or an S3 bucket.

## Install

The chart's default image is `ghcr.io/xeome/slimserve:1.1.0`, published by the release pipeline.
To run an image you built from the repository's `Dockerfile`, set `image.main.repository` and
`image.main.tag` in your values file. For an image loaded straight into a local cluster, also set
`image.main.pullPolicy: Never`.

`host` is required: set it to the public hostname, for the URLs the app builds.

```sh
kubectl create namespace slimserve  # if it does not exist
helm install files oci://ghcr.io/xeome/slimserve-helm --version 3.0.0 -n slimserve -f values.yaml --wait
# or, from a checkout of a release tag:
helm install files ./manifests/helm -n slimserve -f values.yaml --wait
```

Install the published chart, or a checkout of a release tag. A checkout of `master` can name an
image version that is not published yet, so its pod cannot pull the image.

The workload goes in the Helm release namespace. A release in `default` is the exception: its
workload goes in `slimserve`, so create that namespace before you install or upgrade.

The chart needs Kubernetes 1.30 or later. The values file is checked against `values.schema.json`,
so a misspelt key fails the install.

## Storage

The pod mounts `/data` from the `subpath` folder of `<release>-data-pvc`, with `fsGroup: 1001`.
The volume must support that group, or already let UID and GID 1001 read the files, and write them
if you turn on browser edits. `persistence.enabled` defaults to on for local storage and off on S3.
On local storage, `persistence.enabled: false` puts the files in an `emptyDir`, which vanishes with
the pod, so use it only for a throwaway install.

Local storage runs one replica by default. `replicas` above 1 needs persistence on and the cluster
settings under [Replicas](#replicas). Every pod mounts the same `ReadWriteOnce` claim, so the chart
pins every pod to the Kubernetes node that holds it, with a required `podAffinity` on
`app: <release>-main`. Your own `affinity.nodeAffinity` and `affinity.podAntiAffinity` still apply,
but the chart refuses an `affinity.podAffinity` on local storage. An upgrade starts one new pod
beside the old ones on that node, so the node needs room for one more pod, or the rollout waits.

The disk's node is the shape's single point of failure. A drain of that node is planned downtime:
the chart sets no PodDisruptionBudget on local storage, because the pinned pods cannot move. When
the node dies, its pods stay `Terminating`, and the pin holds their replacements `Pending`, until
you run `kubectl delete pod <pod> --force --grace-period=0` for each old pod.

To switch from S3 to local storage, at any replica count, first scale to 0 (`--set replicas=0`),
switch, then set the replica count again. Otherwise the pin places new pods beside the old S3 pods,
on nodes that may not reach the claim, and they stall. A storage switch is planned downtime.

The root filesystem is read-only. The pod runs as UID and GID 1001 with no capabilities, and
writes only to `/data`, `/cache` on S3, and an `emptyDir` on `/tmp` for upload temp files, the
thumbnail cache and crash dumps. So `thumbnail.cacheDir` must sit under `/tmp`, or `/cache` on S3.
The pod uses its own ServiceAccount with no API token mounted.

The chart sets no memory limit. If you set `resources.limits.main.memory`, size it for the Erlang
VM on your nodes: 512 MiB, the old Go chart's limit, can keep it from starting.

## Network and TLS

`port` sets the port the app listens on, and the probes use it. `service.type` and `service.ports`
set how it is exposed. The Service always targets the app's port.

The chart sets `SLIMSERVE_HTTPS=true`, so the app redirects plain HTTP to HTTPS. It does not create
an Ingress or terminate TLS. Keep the HTTP backend private, behind a TLS terminator you trust. That
terminator must pass the public `Host` through and replace any `X-Forwarded-Proto` the client sent
with the scheme of the original request.

Set `trustedProxies` to the addresses or CIDRs of that terminator and any proxy in front of it. The
login limit then counts attempts per client address from `X-Forwarded-For`, read right to left past
those proxies. Without it, every login through the proxy counts against the proxy's address.

The probes call `/.app/healthz` (startup and liveness) and `/.app/readyz` (readiness). On shutdown
a pod waits 5 s for the Service to drop it, accepts new requests for 5 s more, then lets open
requests finish. The grace period is 30 s, so a download still running after about 20 s is cut, and
a browser upload still in flight is cut too, and the user sends it again.

`helm test <release>` checks `/.app/healthz` through the Service.

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

To keep credentials out of your values and the release records, create a Secret yourself and set
`existingSecret` to its name. The chart then writes no Secret, and the pod reads these keys from
yours: `SLIMSERVE_PASSWORD`, `SLIMSERVE_WRITE_PASSWORD`, `SECRET_KEY_BASE`, `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY` and `RELEASE_COOKIE`. A key your settings need (the S3 keys on S3, the
cookie and `SECRET_KEY_BASE` above 1 replica, `SLIMSERVE_PASSWORD` and `SECRET_KEY_BASE` with a
viewer login) must be present, or the pod does not start (`CreateContainerConfigError`). The others
are optional: `SLIMSERVE_WRITE_PASSWORD` turns on browser edits when present. `auth.username` stays
in your values, and a Secret with a `SLIMSERVE_PASSWORD` key needs it set, or the app refuses to start. To rotate a credential, edit the Secret, then run
`kubectl rollout restart deployment/<release>-main -n <namespace>`.

```sh
kubectl create secret generic files-credentials -n slimserve \
  --from-literal=SLIMSERVE_PASSWORD=... --from-literal=SECRET_KEY_BASE="$(openssl rand -base64 48)"
```

## Serving an S3 bucket

Set `storage.backend: s3`, `s3.bucket`, `s3.accessKeyId` and `s3.secretAccessKey`. For anything
but AWS, set `s3.endpoint` to the store's URL (requests are path-style). Set `s3.region` if it is
not `us-east-1`. AWS itself is untested. If you try it, set `s3.endpoint` to the bucket region's
endpoint (`https://s3.<region>.amazonaws.com`), because a bucket that redirects to its regional
host answers every request with a 502. The [root README](../../README.md#serving-an-s3-bucket) lists
what the store must support.

The pod then mounts no `/data`. An S3 release keeps an existing data claim, unmounted, only with
`persistence.enabled=true`, so switching back to local finds its files. The chart refuses an upgrade
that would drop the claim, but only on a real upgrade or `--dry-run=server`. A plain `--dry-run`
and `helm template` do not see the claim. Argo CD renders with `helm template`, so with pruning on,
Argo CD deletes the claim and any files in it.

Each pod caches fetched files up to 100 MB in an `emptyDir` capped at `s3.downloadCacheMB` (default
1024).

Over S3, `replicas` defaults to 2. The pods spread across nodes and zones where they can, and a
PodDisruptionBudget keeps all but one running through a node drain. Set `replicas: 1` to run one
pod without the cluster settings below.

## Replicas

More than one replica, on either storage, needs `cluster.cookie`, a shared secret the pods join
their Erlang cluster with, and a fixed `auth.secretKeyBase`, so a page served by one pod can
reconnect to any other. With `existingSecret`, both come from that Secret instead. The pods find
each other through the headless `<release>-nodes` Service on port 4370, which carries no HTTP. The
HTTP Service is unchanged.

Above 1 replica the chart adds a NetworkPolicy: the HTTP port is open to all, and the cluster port
4370 only to the release's own pods. It takes effect only on a CNI that enforces NetworkPolicy.
Traffic between the pods is not encrypted, so keep the cluster network private. Set
`networkPolicy.enabled: false` to write your own.

A single replica never joins a cluster. So during its rollout, for about 20 s, a change made
through the old pod may show late on the new one. Two replicas avoid that.

## Upgrading from chart 2 to chart 3

Chart 3 needs a SlimServe image that serves `/.app/healthz`, the one it defaults to. If you set
`image.main.tag`, raise it to that release or later.

1. On S3, `replicas` now defaults to 2. Set `cluster.cookie` and `auth.secretKeyBase`, or
   `replicas: 1`.
2. Upgrade with `--reset-then-reuse-values` or `--reset-values -f values.yaml`. Never use
   `--reuse-values`: it drops the defaults of the new keys.
3. An S3 release that already ran several replicas does not cluster across its old and new pods
   during this one rollout, because the cluster port changes to 4370. On a CNI that enforces
   NetworkPolicy, the new policy also cuts the old pods' own cluster until they are gone. So a
   change made through an old pod may show late on another pod. When the upgrade finishes, run
   `kubectl rollout restart deployment/<release>-main -n <namespace>`: the fresh pods start with
   empty caches and cluster on 4370.
4. To switch from S3 to local storage in the same upgrade, scale to 0 first, at any replica count,
   as [Storage](#storage) says.
5. A container now runs no Erlang distribution unless `RELEASE_DISTRIBUTION` is `name` or
   `sname`, and with distribution on it refuses to start without `RELEASE_COOKIE`. So a remote
   shell on a single container needs both set. A chart release with 1 replica does not set
   `RELEASE_DISTRIBUTION`, so `bin/slimserve remote`, `rpc`, `stop` and `pid` cannot reach its pod.
   Above 1 replica the chart sets both.
6. An S3 release from chart 2 has a data claim it does not use. If
   `kubectl get pvc <release>-data-pvc` shows it Pending, it never held files: delete it before the
   upgrade. If it is Bound, it may hold files from local storage: set `persistence.enabled=true` to
   keep it. A values file copied from chart 2's `helm show values` carries
   `persistence.enabled: true`. On S3, remove that line unless you keep a Bound claim on purpose.
   Otherwise the deleted claim comes back Pending and `--wait` hangs. A renderer without cluster
   access, such as Argo CD with pruning on or `helm template | kubectl apply --prune`, gets no
   refusal and deletes the claim. So if the claim is Bound and holds files, set
   `persistence.enabled=true` before you sync.

## Upgrading from the Go chart

Chart 2 and later run the Elixir image. You can upgrade a Go release in place and keep its data.

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
   To keep them out of your values, put them in a Secret of your own under the key names in
   [Login and secrets](#login-and-secrets) and set `existingSecret` to its name.
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
