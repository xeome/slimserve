# The S3 suite needs MinIO: scripts/test-s3.sh runs it with `mix test --only s3`.
# A wait on a signal the app always sends fails only on a hang.
ExUnit.start(exclude: [:s3, :helm], assert_receive_timeout: 5_000)
