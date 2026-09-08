#!/usr/bin/env bash

# This script fails if the desktop runtime archive is larger than the allowed
# maximum. The runtime is embedded into Go binaries via the runtime module, so
# growth needs to be a deliberate decision.
#
# Usage: ./check_size.sh <path.tar.zst> [max_bytes]
#
# The default maximum is 6 MiB.

set -euo pipefail

# Minimal helpers so this script has no dependency on the rest of the repo.
log() {
	echo "$*" 1>&2
}
error() {
	log "ERROR: $*"
	exit 1
}
logrun() {
	log "$ $*"
	"$@"
}
dependencies() {
	for dep in "$@"; do
		if ! command -v "$dep" >/dev/null 2>&1; then
			error "The '$dep' dependency is required, but is not available."
		fi
	done
}

if [[ "$#" -lt 1 ]] || [[ "$#" -gt 2 ]]; then
	error "Usage: $0 <path.tar.zst> [max_bytes]"
fi

path="$1"
max_bytes="${2:-6291456}"

if [[ ! -f "$path" ]]; then
	error "'$path' does not exist"
fi
if [[ ! "$max_bytes" =~ ^[0-9]+$ ]]; then
	error "Invalid max_bytes '$max_bytes', must be a positive integer"
fi

bytes="$(stat -c %s "$path")"
log "$path: $bytes bytes (max $max_bytes bytes)"

if [[ "$bytes" -gt "$max_bytes" ]]; then
	error "'$path' is $((bytes - max_bytes)) bytes over the limit"
fi
