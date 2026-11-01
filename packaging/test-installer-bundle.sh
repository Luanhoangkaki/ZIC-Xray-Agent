#!/bin/bash
# Run functional/mock tests only. Does not install/start any systemd service.
set -euo pipefail
umask 077
[[ $(uname -s) == Linux ]] || { printf '%s\n' 'Functional test binaries require Linux.' >&2; exit 1; }
case $(uname -m) in x86_64) arch=amd64;;aarch64|arm64) arch=arm64;;*) exit 1;;esac
root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
(cd "$root" && sha256sum --check --strict SHA256SUMS)
result=$(mktemp -d "${TMPDIR:-/tmp}/zic-installer-functional.XXXXXXXX")
trap 'printf "Functional evidence: %s\n" "$result"' EXIT
[[ -x /usr/bin/systemd-analyze ]] || { printf '%s\n' 'Requires systemd-analyze from Debian 12.' >&2; exit 1; }
core=${XRAY_BINARY:-"$root/validation/linux-$arch/xray"}
install -m 0700 -- "$core" "$result/xray"
actual=$(sha256sum "$result/xray")
expected=$(sha256sum "$root/validation/linux-$arch/xray")
[[ ${actual%% *} == "${expected%% *}" ]] || { printf '%s\n' 'Test core differs from the verified official packaged core.' >&2; exit 1; }
mkdir -m 0700 -- "$result/empty-confdir"
export XRAY_BINARY="$result/xray" GOMAXPROCS=1 XRAY_LOCATION_CONFDIR="$result/empty-confdir" XRAY_LOCATION_ASSET="$result"
unset ZIC_DAEMON_CRASH_HELPER ZIC_SEND_CRASH_HELPER
bash "$root/source/scripts/test-installer.sh" > "$result/installer-mocks.log" 2>&1
install -m 0700 -- "$root/bin/linux-$arch/zic-xray-agent" "$result/zic-xray-agent"
awk -v binary="$result/zic-xray-agent" '{if($0~/^ExecStart=/)sub(/^ExecStart=[^ ]+/,"ExecStart=" binary);print}' "$root/packaging/zic-xray-agent.service" > "$result/zic-xray-agent.service"
systemd-analyze verify "$result/zic-xray-agent.service" > "$result/unit-verify.log" 2>&1
tests=("$root/tests/"*.test)
[[ ${#tests[@]} == 12 ]] || { printf '%s\n' 'Expected exactly 12 functional suites.' >&2; exit 1; }
for test in "${tests[@]}"; do
    # Copy into private temp; do not change any installed binary or its mode.
    name=$(basename -- "$test")
    install -m 0700 -- "$test" "$result/$name"
    env -u xray.location.confdir -u xray.location.config -u xray.location.asset "$result/$name" -test.v -test.timeout=120s > "$result/$name.log" 2>&1
done
printf '%s\n' 'Functional suites PASS. This does not prove real installer/systemd/TTY/VPS acceptance or production readiness.'
