#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
artifact_package="${PORTABLEDESKTOP_BUILD_PACKAGE:-build}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [--package <flake-package>]

Builds the primary Nix artifact and validates:
  result/output.tar
  result/output/
  result/manifest.json

Defaults:
  package: build

Overrides:
  --package <flake-package>
  PORTABLEDESKTOP_BUILD_PACKAGE=<flake-package>
USAGE
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "error: missing required command: ${cmd}" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --package)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "error: --package requires a value" >&2
        exit 1
      fi
      artifact_package="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_cmd nix
require_cmd sha256sum
require_cmd stat

(
  cd "${repo_root}"
  nix build --no-write-lock-file ".#${artifact_package}"
)

archive_path="${repo_root}/result/output.tar"
output_dir="${repo_root}/result/output"
manifest_path="${repo_root}/result/manifest.json"

if [[ ! -s "${archive_path}" ]]; then
  echo "error: missing or empty archive: ${archive_path}" >&2
  exit 1
fi

if [[ ! -d "${output_dir}" ]]; then
  echo "error: missing output directory: ${output_dir}" >&2
  exit 1
fi

if [[ ! -s "${manifest_path}" ]]; then
  echo "error: missing or empty manifest: ${manifest_path}" >&2
  exit 1
fi

archive_sha256="$(sha256sum "${archive_path}" | awk '{print $1}')"
archive_size_bytes="$(stat -c '%s' "${archive_path}")"

manifest_sha256="$(sed -nE 's/.*"archive_sha256"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "${manifest_path}" | head -n1)"
if [[ -z "${manifest_sha256}" ]]; then
  echo "error: failed to parse archive_sha256 from ${manifest_path}" >&2
  exit 1
fi
if [[ "${manifest_sha256}" != "${archive_sha256}" ]]; then
  echo "error: manifest/archive sha256 mismatch" >&2
  echo "manifest: ${manifest_sha256}" >&2
  echo "archive:  ${archive_sha256}" >&2
  exit 1
fi

echo "package: ${artifact_package}"
echo "archive: ${archive_path}"
echo "output: ${output_dir}"
echo "manifest: ${manifest_path}"
echo "archive sha256: ${archive_sha256}"
echo "archive size bytes: ${archive_size_bytes}"
