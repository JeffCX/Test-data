#!/usr/bin/env bash
# Download the pinned Linux x86_64 release, verify it, then run it.
set -euo pipefail
umask 077

url='https://github.com/JeffCX/Test-data/releases/download/v0.1.0/codex-snapshot-linux-amd64'
sha256='3d35e0152760b767531bc90cc393154065929a9302715d3c9e433c3db03840d6'

if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
  printf 'This script requires Linux x86_64.\n' >&2
  exit 1
fi
if [[ ! -d /home/agent/.codex || ! -d /home/agent/workspace ]]; then
  printf 'Required directories /home/agent/.codex and /home/agent/workspace were not found.\n' >&2
  exit 1
fi
if ! command -v sha256sum >/dev/null 2>&1; then
  printf 'sha256sum is required.\n' >&2
  exit 1
fi

tmpdir=$(mktemp -d /home/agent/.codex-snapshot-run.XXXXXXXX)
binary="$tmpdir/codex-snapshot-linux-amd64"
cleanup() {
  [[ ! -e "$binary" ]] || rm -- "$binary"
  rmdir -- "$tmpdir"
}
trap cleanup EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fL --retry 3 --connect-timeout 10 --max-time 120 \
    --proto '=https' --proto-redir '=https' -o "$binary" "$url"
elif command -v wget >/dev/null 2>&1; then
  wget --https-only -O "$binary" "$url"
else
  printf 'curl or wget is required to download the release.\n' >&2
  exit 1
fi

printf '%s  %s\n' "$sha256" "$binary" | sha256sum -c -
chmod 700 "$binary"
"$binary"
