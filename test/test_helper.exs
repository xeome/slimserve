# The S3 suite needs MinIO: scripts/test-s3.sh runs it with `mix test --only s3`.
ExUnit.start(exclude: [:s3, :helm])
