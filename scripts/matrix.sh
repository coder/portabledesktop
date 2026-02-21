#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
smoke_script="${script_dir}/smoke.sh"
build_script="${script_dir}/build.sh"

artifact_package="${PORTABLEDESKTOP_BUILD_PACKAGE:-build}"
base_port="${PORTABLEDESKTOP_MATRIX_BASE_PORT:-16001}"
timeout_seconds="${PORTABLEDESKTOP_MATRIX_TIMEOUT_SECONDS:-45}"
skip_build="0"
fail_fast="0"
keep_failure_containers="0"

default_images=(
  "ubuntu:24.04"
  "ubuntu:22.04"
  "debian:12-slim"
  "debian:11-slim"
  "fedora:41"
  "rockylinux:9"
)
images=()

usage() {
  cat <<USAGE
Usage: $(basename "$0") [options]

Runs smoke tests across a Linux distro matrix.

Options:
  --package <flake-package>            Build package (default: build)
  --image <docker-image>               Add a matrix image (repeatable)
  --base-port <port>                   Base host port (default: 16001)
  --timeout-seconds <seconds>          Per-image handshake timeout (default: 45)
  --skip-build                         Skip build (expects result/output)
  --fail-fast                          Stop on first failure
  --keep-failure-containers            Keep failed containers for inspection
  -h, --help                           Show help

Env overrides:
  PORTABLEDESKTOP_BUILD_PACKAGE
  PORTABLEDESKTOP_MATRIX_BASE_PORT
  PORTABLEDESKTOP_MATRIX_TIMEOUT_SECONDS
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
    --image)
      require_option_value "--image" "${2:-}"
      images+=("${2:-}")
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
    --skip-build)
      skip_build="1"
      shift
      ;;
    --fail-fast)
      fail_fast="1"
      shift
      ;;
    --keep-failure-containers)
      keep_failure_containers="1"
      shift
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

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required for matrix testing" >&2
  exit 1
fi

if [[ ! -x "${smoke_script}" ]]; then
  echo "error: smoke script missing or not executable: ${smoke_script}" >&2
  exit 1
fi

if [[ "${#images[@]}" -eq 0 ]]; then
  images=("${default_images[@]}")
fi

if [[ "${skip_build}" != "1" ]]; then
  "${build_script}" --package "${artifact_package}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

declare -a result_rows=()
passed=0
failed=0
index=0

for image in "${images[@]}"; do
  port="$((base_port + index))"
  container_name="portabledesktop-matrix-${index}"
  log_path="${tmp_dir}/${index}.log"

  echo "testing image=${image} port=${port} container=${container_name}"

  if "${smoke_script}" \
      --skip-build \
      --image "${image}" \
      --container-name "${container_name}" \
      --host-port "${port}" \
      --timeout-seconds "${timeout_seconds}" \
      >"${log_path}" 2>&1; then
    status="PASS"
    passed=$((passed + 1))
    docker rm -f "${container_name}" >/dev/null 2>&1 || true
  else
    status="FAIL"
    failed=$((failed + 1))
    if [[ "${keep_failure_containers}" != "1" ]]; then
      docker rm -f "${container_name}" >/dev/null 2>&1 || true
    fi
    result_rows+=("${status}|${image}|${port}|${container_name}|${log_path}")
    if [[ "${fail_fast}" == "1" ]]; then
      break
    fi
    index=$((index + 1))
    continue
  fi

  result_rows+=("${status}|${image}|${port}|${container_name}|${log_path}")
  index=$((index + 1))
done

echo
printf '%-6s %-18s %-6s %-30s\n' "STATUS" "IMAGE" "PORT" "CONTAINER"
printf '%-6s %-18s %-6s %-30s\n' "------" "------------------" "------" "------------------------------"
for row in "${result_rows[@]}"; do
  status="$(echo "${row}" | cut -d'|' -f1)"
  image="$(echo "${row}" | cut -d'|' -f2)"
  port="$(echo "${row}" | cut -d'|' -f3)"
  container_name="$(echo "${row}" | cut -d'|' -f4)"
  printf '%-6s %-18s %-6s %-30s\n' "${status}" "${image}" "${port}" "${container_name}"
done

if [[ "${failed}" -gt 0 ]]; then
  echo
  echo "failed image logs:"
  for row in "${result_rows[@]}"; do
    status="$(echo "${row}" | cut -d'|' -f1)"
    image="$(echo "${row}" | cut -d'|' -f2)"
    log_path="$(echo "${row}" | cut -d'|' -f5)"
    if [[ "${status}" == "FAIL" ]]; then
      echo "----- ${image} -----"
      cat "${log_path}" >&2
      echo >&2
    fi
  done
fi

echo
echo "matrix summary: passed=${passed} failed=${failed} total=$((passed + failed))"

if [[ "${failed}" -gt 0 ]]; then
  exit 1
fi
