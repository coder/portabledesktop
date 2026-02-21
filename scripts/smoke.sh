#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
build_script="${script_dir}/build.sh"

artifact_package="${PORTABLEDESKTOP_BUILD_PACKAGE:-build}"
container_name="${PORTABLEDESKTOP_SMOKE_CONTAINER_NAME:-portabledesktop-smoke}"
container_image="${PORTABLEDESKTOP_SMOKE_IMAGE:-ubuntu:24.04}"
host_port="${PORTABLEDESKTOP_SMOKE_HOST_PORT:-15901}"
container_rfb_port="${PORTABLEDESKTOP_SMOKE_CONTAINER_RFB_PORT:-5901}"
display="${PORTABLEDESKTOP_SMOKE_DISPLAY:-:1}"
geometry="${PORTABLEDESKTOP_SMOKE_GEOMETRY:-1280x800}"
depth="${PORTABLEDESKTOP_SMOKE_DEPTH:-24}"
timeout_seconds="${PORTABLEDESKTOP_SMOKE_TIMEOUT_SECONDS:-45}"
runtime_dir_override="${PORTABLEDESKTOP_SMOKE_RUNTIME_DIR:-}"
strict_log_checks="${PORTABLEDESKTOP_SMOKE_STRICT_LOG_CHECKS:-1}"
skip_build="0"
keep_running="0"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [options]

Builds (unless --skip-build), launches Xvnc from result/output in a bare
container, and verifies an RFB handshake from the host.

Options:
  --package <flake-package>            Build package (default: build)
  --runtime-dir <path>                 Runtime root mount (default: result/output)
  --container-name <name>              Docker container name (default: portabledesktop-smoke)
  --image <docker-image>               Container image (default: ubuntu:24.04)
  --host-port <port>                   Host forwarded VNC port (default: 15901)
  --container-rfb-port <port>          Container VNC port (default: 5901)
  --display <display>                  X display (default: :1)
  --geometry <width>x<height>          VNC geometry (default: 1280x800)
  --depth <bits>                       VNC depth (default: 24)
  --timeout-seconds <seconds>          Handshake timeout (default: 45)
  --no-strict-log-checks               Allow XKB startup warnings
  --skip-build                         Skip build (expects result/output)
  --keep-running                       Keep container alive after success
  -h, --help                           Show help

Env overrides:
  PORTABLEDESKTOP_BUILD_PACKAGE
  PORTABLEDESKTOP_SMOKE_RUNTIME_DIR
  PORTABLEDESKTOP_SMOKE_CONTAINER_NAME
  PORTABLEDESKTOP_SMOKE_IMAGE
  PORTABLEDESKTOP_SMOKE_HOST_PORT
  PORTABLEDESKTOP_SMOKE_CONTAINER_RFB_PORT
  PORTABLEDESKTOP_SMOKE_DISPLAY
  PORTABLEDESKTOP_SMOKE_GEOMETRY
  PORTABLEDESKTOP_SMOKE_DEPTH
  PORTABLEDESKTOP_SMOKE_TIMEOUT_SECONDS
  PORTABLEDESKTOP_SMOKE_STRICT_LOG_CHECKS
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
    --runtime-dir)
      require_option_value "--runtime-dir" "${2:-}"
      runtime_dir_override="${2:-}"
      shift 2
      ;;
    --container-name)
      require_option_value "--container-name" "${2:-}"
      container_name="${2:-}"
      shift 2
      ;;
    --image)
      require_option_value "--image" "${2:-}"
      container_image="${2:-}"
      shift 2
      ;;
    --host-port)
      require_option_value "--host-port" "${2:-}"
      host_port="${2:-}"
      shift 2
      ;;
    --container-rfb-port)
      require_option_value "--container-rfb-port" "${2:-}"
      container_rfb_port="${2:-}"
      shift 2
      ;;
    --display)
      require_option_value "--display" "${2:-}"
      display="${2:-}"
      shift 2
      ;;
    --geometry)
      require_option_value "--geometry" "${2:-}"
      geometry="${2:-}"
      shift 2
      ;;
    --depth)
      require_option_value "--depth" "${2:-}"
      depth="${2:-}"
      shift 2
      ;;
    --timeout-seconds)
      require_option_value "--timeout-seconds" "${2:-}"
      timeout_seconds="${2:-}"
      shift 2
      ;;
    --no-strict-log-checks)
      strict_log_checks="0"
      shift
      ;;
    --skip-build)
      skip_build="1"
      shift
      ;;
    --keep-running)
      keep_running="1"
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
  echo "error: docker is required for smoke testing" >&2
  exit 1
fi

if [[ "${skip_build}" != "1" ]]; then
  "${build_script}" --package "${artifact_package}"
fi

if [[ -n "${runtime_dir_override}" ]]; then
  runtime_dir="$(readlink -f "${runtime_dir_override}")"
else
  runtime_dir="$(readlink -f "${repo_root}/result/output")"
fi
launcher_path="${runtime_dir}/bin/Xvnc"

if [[ ! -d "${runtime_dir}" ]]; then
  echo "error: runtime directory missing: ${runtime_dir}" >&2
  exit 1
fi

if [[ ! -x "${launcher_path}" ]]; then
  echo "error: runtime launcher missing or not executable: ${launcher_path}" >&2
  exit 1
fi

cleanup_container() {
  docker rm -f "${container_name}" >/dev/null 2>&1 || true
}

cleanup_container

docker run -d \
  --name "${container_name}" \
  -p "${host_port}:${container_rfb_port}" \
  -v "${runtime_dir}:/runtime:ro" \
  "${container_image}" \
  bash -lc "exec /runtime/bin/Xvnc ${display} -geometry ${geometry} -depth ${depth} -rfbport ${container_rfb_port} -SecurityTypes None -ac -nolisten tcp -localhost no" \
  >/dev/null

check_banner() {
  timeout 2 bash -lc "exec 3<>/dev/tcp/127.0.0.1/${host_port}; head -c 12 <&3" 2>/dev/null || true
}

deadline=$((SECONDS + timeout_seconds))
banner=""
while (( SECONDS < deadline )); do
  if [[ -z "$(docker ps --filter "name=^/${container_name}$" --filter "status=running" --quiet)" ]]; then
    echo "error: container exited before handshake: ${container_name}" >&2
    docker logs --tail 120 "${container_name}" >&2 || true
    exit 1
  fi

  banner="$(check_banner)"
  if [[ "${banner}" == RFB* ]]; then
    break
  fi
  sleep 1
done

if [[ "${banner}" != RFB* ]]; then
  echo "error: timed out waiting for VNC handshake on 127.0.0.1:${host_port}" >&2
  docker logs --tail 120 "${container_name}" >&2 || true
  exit 1
fi

if [[ "${strict_log_checks}" == "1" ]]; then
  log_output="$(docker logs --tail 300 "${container_name}" 2>&1 || true)"
  if echo "${log_output}" | grep -qE 'Failed to activate virtual core keyboard|XKB: Failed to compile keymap|xkbcomp\) reports:|> Error:'; then
    echo "error: strict log checks failed for container: ${container_name}" >&2
    echo "${log_output}" >&2
    exit 1
  fi
fi

echo "container: ${container_name}"
echo "image: ${container_image}"
echo "runtime: ${runtime_dir}"
echo "host endpoint: 127.0.0.1:${host_port}"
echo "vnc banner: ${banner}"

if [[ "${keep_running}" == "1" ]]; then
  echo "status: running (kept alive for manual testing)"
  exit 0
fi

cleanup_container
echo "status: passed (container removed)"
