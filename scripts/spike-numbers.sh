#!/usr/bin/env bash
# Compares the Go image (built from master) with this checkout's Elixir image: image size, idle RSS,
# and peak RSS while one 1 GiB file streams. Prints a markdown table.
# Needs docker, git and curl, with the docker daemon's /proc visible here (a local Linux daemon).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

file_bytes=1073741824
containers=()
data=""
notes=""
declare -A size_mib idle_mib peak_mib

cleanup() {
  if ((${#containers[@]})); then docker rm -f "${containers[@]}" >/dev/null 2>&1 || true; fi
  if [[ -n $data ]]; then rm -rf -- "$data"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

log() { printf '%s\n' "$*" >&2; }
die() {
  log "$*"
  exit 1
}
mib() { awk -v n="$1" -v d="$2" 'BEGIN { printf "%.1f", n / d }'; }

# Sets rss to the sum of VmRSS (KiB) over the given pids. Not docker stats: its cgroup figure
# counts the page cache the 1 GiB read fills. Builtins only, so a 50 ms loop stays cheap.
rss_kb() {
  local p k v _
  rss=0
  for p in "$@"; do
    while read -r k v _; do
      if [[ $k == VmRSS: ]]; then rss=$((rss + v)); fi
    done 2>/dev/null <"/proc/$p/status" || true
  done
}

# measure LABEL COMM IMAGE [ARGS...]: runs IMAGE alone and fills size_mib, idle_mib and peak_mib for
# LABEL. COMM must name one of the container's processes, so a wrong pid set fails loudly.
measure() {
  local label=$1 comm=$2 image=$3
  shift 3
  local c port url i p ready=0 found=0 pids=() samples=() idle_kb peak_kb=0 cpid got

  size_mib[$label]=$(mib "$(docker image inspect -f '{{.Size}}' "$image")" 1048576)

  # --no-healthcheck: master's wget probe would add a transient process to Go only.
  c=$(docker run -d --no-healthcheck -p 127.0.0.1::8080 -v "$data/srv:/data" "$image" "$@")
  containers+=("$c")
  if ! port=$(docker port "$c" 8080/tcp | head -n 1) || [[ -z $port ]]; then
    docker logs "$c" >&2
    die "$label: docker port found no mapping for 8080"
  fi
  # Host localhost: the Elixir prod config exempts it from force_ssl.
  url=http://localhost:${port##*:}

  for ((i = 0; i < 60; i++)); do
    if [[ $(curl -s --max-time 2 -o /dev/null -w '%{http_code}' "$url/") == 200 ]]; then
      ready=1
      break
    fi
    sleep 1
  done
  if ((!ready)); then
    docker logs "$c" >&2
    die "$label: / did not answer 200 within 60 s"
  fi

  mapfile -t pids < <(docker top "$c" -o pid | tail -n +2)
  for p in "${pids[@]}"; do
    if [[ $(<"/proc/$p/comm") == "$comm" ]]; then found=1; fi
  done
  if ((!found)); then die "$label: no $comm process among pids ${pids[*]}"; fi

  log "$label: settling 10 s, then 10 idle samples"
  sleep 10
  for ((i = 0; i < 10; i++)); do
    rss_kb "${pids[@]}"
    samples+=("$rss")
    sleep 1
  done
  idle_kb=$(printf '%s\n' "${samples[@]}" | sort -n | awk 'NR == 5 || NR == 6 { s += $1 } END { print s / 2 }')
  idle_mib[$label]=$(mib "$idle_kb" 1024)

  log "$label: streaming 1 GiB"
  curl -sS --max-time 600 -o /dev/null -w '%{size_download}' "$url/1g.bin" >"$data/curl.out" &
  cpid=$!
  # ponytail: 50 ms sampling can miss a shorter spike; the VmHWM notes bound it from above.
  while kill -0 "$cpid" 2>/dev/null; do
    rss_kb "${pids[@]}"
    if ((rss > peak_kb)); then peak_kb=$rss; fi
    sleep 0.05
  done
  if ! wait "$cpid"; then die "$label: the download failed"; fi
  got=$(<"$data/curl.out")
  if [[ $got != "$file_bytes" ]]; then die "$label: downloaded $got bytes, want $file_bytes"; fi
  peak_mib[$label]=$(mib "$peak_kb" 1024)

  for p in "${pids[@]}"; do
    [[ -r /proc/$p/status ]] || continue
    notes+="- $label $(<"/proc/$p/comm") (pid $p): VmHWM $(awk '/^VmHWM:/ { print $2 }' "/proc/$p/status") KiB"$'\n'
  done

  docker rm -f "$c" >/dev/null
}

# Master's Dockerfile sits at the root of its tree, so the archive is the build context.
log "building slimserve-go from master"
git archive master | docker build -q -t slimserve-go - >/dev/null
log "building slimserve-ex from this checkout"
docker build -q -t slimserve-ex . >/dev/null

# Master's Dockerfile pins GOARCH=amd64. On another host Go runs under qemu, so its RSS is inflated.
case $(uname -m) in
  x86_64) host_arch=amd64 ;;
  aarch64) host_arch=arm64 ;;
  *) host_arch=$(uname -m) ;;
esac
for image in slimserve-go slimserve-ex; do
  arch=$(docker image inspect -f '{{.Architecture}}' "$image")
  if [[ $arch != "$host_arch" ]]; then die "$image is $arch but the host is $host_arch"; fi
done

# On disk, not /tmp, which may be tmpfs and would hold the file in RAM. Both images run as uid 1001,
# so the served tree is world-readable.
cache=${XDG_CACHE_HOME:-$HOME/.cache}
mkdir -p "$cache"
data=$(mktemp -d "$cache/slimserve-spike.XXXXXX")
mkdir "$data/srv"
chmod 755 "$data" "$data/srv"
log "writing 1 GiB of random data to $data/srv"
head -c "$file_bytes" /dev/urandom >"$data/srv/1g.bin"
chmod 644 "$data/srv/1g.bin"

# Explicit args: master's CMD passes -dirs, a flag its loader no longer has.
measure Go slimserve slimserve-go -port 8080 -storage-path /data
# The image sets SLIMSERVE_ROOT=/data and PHX_SERVER=true.
measure Elixir beam.smp slimserve-ex

cat <<EOF
$(date -u +%F), master $(git rev-parse --short master), this branch $(git describe --always --dirty), nproc $(nproc), storage driver $(docker info -f '{{.Driver}}')

| MiB | Go | Elixir |
|---|---|---|
| Image size | ${size_mib[Go]} | ${size_mib[Elixir]} |
| Idle RSS (median of 10) | ${idle_mib[Go]} | ${idle_mib[Elixir]} |
| Peak RSS streaming 1 GiB | ${peak_mib[Go]} | ${peak_mib[Elixir]} |

Peak VmHWM per process, an upper bound on what 50 ms sampling can miss:

$notes
EOF
