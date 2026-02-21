#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

artifact_package="${PORTABLEDESKTOP_BUILD_PACKAGE:-build}"
base_port="${PORTABLEDESKTOP_MATRIX_BASE_PORT:-16001}"
timeout_seconds="${PORTABLEDESKTOP_MATRIX_TIMEOUT_SECONDS:-45}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [--package <flake-package>] [--base-port <port>] [--timeout-seconds <seconds>]

Runs the full workflow:
  1. Build and validate result/
  2. Smoke test in one container
  3. Cross-distro matrix test
USAGE
}

require_option_value() {
  local option="$1"
  local value="${2:-}"
  if [[ -z "${value}" ]]; then
    echo "error: ${option} requires a value" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --package)
      require_option_value "--package" "${2:-}"
      artifact_package="${2:-}"
      shift 2
      ;;
    --base-port)
      require_option_value "--base-port" "${2:-}"
      base_port="${2:-}"
      shift 2
      ;;
    --timeout-seconds)
      require_option_value "--timeout-seconds" "${2:-}"
      timeout_seconds="${2:-}"
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

"${script_dir}/build.sh" --package "${artifact_package}"
"${script_dir}/smoke.sh" --skip-build --package "${artifact_package}"
"${script_dir}/matrix.sh" --skip-build --package "${artifact_package}" --base-port "${base_port}" --timeout-seconds "${timeout_seconds}"
