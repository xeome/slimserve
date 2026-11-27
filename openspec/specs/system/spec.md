# system Specification

## Purpose

These rules hold for the whole app: how many nodes may serve, and how fast what one shows follows
a change. Four cases are not required. An open page need not follow writers to the bucket other
than SlimServe, because only SlimServe should write the bucket. So a direct bucket edit shows on a
page loaded after the cached listing expires, at most 10 minutes after it was filled. And an
upload whose answer was lost while still in flight may look missing until that expiry. On local
storage, a folder made inside a folder made at the same moment, by `mkdir -p` for example, may not
follow the disk on an open page until the app restarts. A page load still lists it, because a local
listing is read from disk. And an upload still in progress over the page's live connection when a
node shuts down is cut, so the user sends it again.

## Requirements

### Requirement: Nodes

Several replicas MAY serve on either storage, and SHALL reach each other. More than one replica
MUST have a shared cluster cookie and signing key. More than one replica on local storage SHALL
share one persistent volume and run on the Kubernetes node that holds it. On S3 the replicas MAY
spread across nodes.

#### Scenario: Replicas cluster on either storage, with a shared cookie and signing key, beside the HTTP Service

- **WHEN** an operator deploys `replicas: 3`
- **THEN** it is refused without a shared cluster cookie and signing key, and on local storage
  without a persistent volume
- **AND** with them, each replica is named by its address and finds the others, the cluster's
  port stays off the public HTTP Service, and on local storage every replica runs on the
  Kubernetes node that holds the disk

### Requirement: Freshness

On local storage an open listing SHALL follow the disk within a second. On S3, a change made
through SlimServe SHALL show on every node at once. A change made in the bucket directly SHALL
show on any page loaded after the cached listing expires, at most 10 minutes after it was filled.
A page already open shows it only when a write through SlimServe wakes it or the page reloads.

#### Scenario: A write on one node shows at once on another node's open page and next request

- **WHEN** a node has listed a folder and looked up a missing name, and a write through another
  node makes a folder of that name
- **THEN** the first node's open page lists the folder within a second
- **AND** its next lookup finds the folder

### Requirement: Go settings refuse the boot

A start SHALL refuse to boot, and name what it found, when a setting or config file that only the
Go app read is present. So an upgrade from the Go app does not run on without its settings, and
does not serve the Go `slimserve.json`, logins included, as a plain file.

#### Scenario: A setting only the Go app read refuses the boot and names it

- **WHEN** an operator starts SlimServe with `SLIMSERVE_CONFIG=/x`
- **THEN** the start fails with a message that names `SLIMSERVE_CONFIG` and links the 1.0.0
  release notes

### Requirement: Health endpoints

Every node SHALL answer `GET /.app/healthz` with 200 while it runs, and `GET /.app/readyz` with 200
while it takes new requests. Both SHALL answer without login, over plain HTTP with HTTPS on, and
without reaching the bucket.

#### Scenario: The health paths answer 200 without login and with S3 unreachable

- **WHEN** login is on, the S3 endpoint refuses connections, and a probe asks for
  `/.app/healthz` and `/.app/readyz`
- **THEN** both answer 200
- **AND** `/` still answers 401

### Requirement: Clean shutdown

On SIGTERM a node SHALL answer `/.app/readyz` with 503 and keep accepting requests for 5 seconds,
so a proxy stops sending to it first. It SHALL then stop accepting and let every open HTTP request
other than the page's live connection, such as a download, run to its end.

#### Scenario: After SIGTERM, /.app/readyz answers 503 while an open download completes

- **WHEN** a client is part way through a 32 MB download and the node gets SIGTERM
- **THEN** `/.app/readyz` answers 503
- **AND** the download still receives every byte

### Requirement: Cluster messages survive a version step

A node SHALL keep serving when another node sends it a cluster message it does not know, as a
node of another version may during a rolling upgrade. An unknown message about S3 changes SHALL
empty that node's S3 cache, so its next request reads the bucket.

#### Scenario: An unknown cluster message empties that node's S3 cache

- **WHEN** a node has a folder listing cached and gets an S3 change message it does not know
- **THEN** the node keeps serving, and its next listing of that folder asks the bucket

### Requirement: Fixed cluster ports

A release SHALL run with no epmd and take cluster connections on port 4370 only, so a network
policy can name the one port. `bin/slimserve remote` and `bin/slimserve rpc` SHALL reach a running
node without taking that port.

#### Scenario: Two replicas cluster through port 4370 with nothing on 4369

- **WHEN** two release containers on one network start with `RELEASE_DISTRIBUTION=name` and one
  cookie, and one connects to the other through `bin/slimserve rpc`
- **THEN** each lists the other
- **AND** each listens on 4370 and nothing listens on 4369
