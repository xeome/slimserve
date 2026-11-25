#!/usr/bin/env bash
# Each test makes its own bucket.
set -euo pipefail

image=minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e
name="slimserve-s3-test-$$"

docker run -d --rm --name "$name" -p 127.0.0.1::9000 \
  -e MINIO_ROOT_USER=slimserve-test -e MINIO_ROOT_PASSWORD=slimserve-test-secret \
  "$image" server /data >/dev/null
# The cluster test starts a second node, so distribution needs a port mapper; stop only one started here.
epmd_started=
if ! epmd -names >/dev/null 2>&1; then epmd -daemon && epmd_started=1; fi
trap 'docker rm -f "$name" >/dev/null; if [ -n "$epmd_started" ]; then epmd -kill >/dev/null; fi' EXIT

endpoint="http://$(docker port "$name" 9000/tcp | head -n1)"
for _ in $(seq 1 50); do
  curl -fsS "$endpoint/minio/health/ready" >/dev/null 2>&1 && break
  sleep 0.2
done

export SLIMSERVE_TEST_S3_ENDPOINT="$endpoint"
export SLIMSERVE_TEST_S3_KEY=slimserve-test SLIMSERVE_TEST_S3_SECRET=slimserve-test-secret
if [ "$#" -eq 0 ]; then set -- mix test --only s3; fi
"$@"
