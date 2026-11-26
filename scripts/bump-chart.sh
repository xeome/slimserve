#!/usr/bin/env bash
set -euo pipefail
chart=manifests/helm/Chart.yaml
guide=manifests/helm/README.md

app=${1:-}
app=${app#v}
[[ $app =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: $0 vX.Y.Z" >&2; exit 2; }
old=$(sed -n -E 's/^appVersion: "?([^"]*)"?$/\1/p' "$chart")
[ "$old" = "$app" ] && exit 0

IFS=. read -r a1 a2 _ <<<"$app"
IFS=. read -r o1 o2 _ <<<"$old"
IFS=. read -r c1 c2 c3 <<<"$(sed -n -E 's/^version: "?([^"]*)"?$/\1/p' "$chart")"
if [ "$a1" != "$o1" ]; then new="$((c1 + 1)).0.0"
elif [ "$a2" != "$o2" ]; then new="$c1.$((c2 + 1)).0"
else new="$c1.$c2.$((c3 + 1))"; fi

sed -i -e "s/^version: .*/version: $new/" -e "s/^appVersion: .*/appVersion: \"$app\"/" "$chart"
sed -i -E -e "s/--version [0-9.]+/--version $new/" -e "s|ghcr\.io/xeome/slimserve:[0-9.]+|ghcr.io/xeome/slimserve:$app|" "$guide"
echo "chart $new for app $app"
