#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
assets_dir="${repo_root}/assets"

materialize_pack_compatible_links() {
  local runtime_root="$1"
  local lib_dir="${runtime_root}/lib"
  local materialized_count=0

  # npm package tarballs omit symlinks, so convert SONAME aliases into real files.
  if [[ -d "${lib_dir}" ]]; then
    while IFS= read -r -d '' link_path; do
      local link_name
      link_name="$(basename "${link_path}")"
      if [[ ! "${link_name}" =~ \.so\.[0-9]+$ ]]; then
        continue
      fi

      local target_path
      target_path="$(readlink -f "${link_path}")"
      rm -f "${link_path}"
      cp -a "${target_path}" "${link_path}"
      materialized_count=$((materialized_count + 1))
    done < <(find "${lib_dir}" -maxdepth 1 -type l -print0)
  fi

  # Keep XKB assets available when share/X11/xkb symlink is dropped.
  local xkb_link="${runtime_root}/share/X11/xkb"
  if [[ -L "${xkb_link}" ]]; then
    local xkb_target
    xkb_target="$(readlink -f "${xkb_link}")"
    rm -f "${xkb_link}"
    mkdir -p "${xkb_link}"
    cp -a "${xkb_target}/." "${xkb_link}/"
    materialized_count=$((materialized_count + 1))
  fi

  echo "materialized links: ${materialized_count}"
}

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
materialize_pack_compatible_links "${assets_dir}/output"
install -m 0644 "${repo_root}/result/manifest.json" "${assets_dir}/manifest.json"

size="$(du -sb "${assets_dir}/output" | awk '{print $1}')"
file_count="$(find "${assets_dir}/output" -type f | wc -l)"

echo "bundled: ${assets_dir}/output"
echo "bundled: ${assets_dir}/manifest.json"
echo "files: ${file_count}"
echo "size bytes: ${size}"
