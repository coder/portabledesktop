#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
assets_dir="${repo_root}/assets"

if [[ ! -s "${repo_root}/result/output.tar" || ! -s "${repo_root}/result/manifest.json" ]]; then
  "${script_dir}/build.sh"
fi

mkdir -p "${assets_dir}"
# Nix-produced files are often read-only (0444). Install with stable writable
# perms in the workspace so repeated prepack runs can overwrite in place.
install -m 0644 "${repo_root}/result/output.tar" "${assets_dir}/output.tar"
install -m 0644 "${repo_root}/result/manifest.json" "${assets_dir}/manifest.json"

sha="$(sha256sum "${assets_dir}/output.tar" | awk '{print $1}')"
size="$(stat -c '%s' "${assets_dir}/output.tar")"

echo "bundled: ${assets_dir}/output.tar"
echo "bundled: ${assets_dir}/manifest.json"
echo "sha256: ${sha}"
echo "size bytes: ${size}"
