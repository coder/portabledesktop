#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
assets_dir="${repo_root}/assets"

if [[ ! -d "${repo_root}/result/output" || ! -s "${repo_root}/result/manifest.json" ]]; then
  "${script_dir}/build.sh"
fi

mkdir -p "${assets_dir}"
if [[ -d "${assets_dir}/output" ]]; then
  chmod -R u+w "${assets_dir}/output" || true
  rm -rf "${assets_dir}/output"
fi
rm -f "${assets_dir}/output.tar"
mkdir -p "${assets_dir}/output"
cp -a "${repo_root}/result/output/." "${assets_dir}/output/"
chmod -R u+w "${assets_dir}/output" || true
install -m 0644 "${repo_root}/result/manifest.json" "${assets_dir}/manifest.json"

size="$(du -sb "${assets_dir}/output" | awk '{print $1}')"
file_count="$(find "${assets_dir}/output" -type f | wc -l)"

echo "bundled: ${assets_dir}/output"
echo "bundled: ${assets_dir}/manifest.json"
echo "files: ${file_count}"
echo "size bytes: ${size}"
