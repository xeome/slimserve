# system Specification

## Purpose

These rules hold for the whole app: how many nodes may serve, and how fast what one shows follows
a change. Two cases are not required. An open page need not follow writers to the bucket other
than SlimServe, because only SlimServe should write the bucket. So a direct bucket edit shows on a
page loaded after the cached listing expires, at most 10 minutes after it was filled. And an
upload whose answer was lost while still in flight may look missing until that expiry.

## Requirements

### Requirement: Nodes

On local storage one node SHALL serve. On S3, several replicas MAY share the bucket and SHALL
reach each other, and more than one replica MUST have a shared cluster cookie.

#### Scenario: Replicas cluster only over S3, with a shared cookie and signing key, beside the HTTP Service

- **WHEN** an operator deploys more than one replica
- **THEN** the deployment is refused on local storage, and on S3 without a shared cluster cookie
  and signing key
- **AND** with both, each replica is named by its address and finds the others, while the
  cluster's port stays off the public HTTP service

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
