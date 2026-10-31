#!/bin/bash
# Explicitly invoked on a disposable test VM only. No installer, service
# activation, production panel request, package installation or core download.
set -euo pipefail
umask 077
[[ ${ZIC_LAB:-} == 1 ]] || { printf '%s\n' 'Refusing: set ZIC_LAB=1 only on an approved disposable Debian 12 test VM.' >&2; exit 1; }
[[ $(uname -s) == Linux && -f /etc/os-release ]] || exit 1
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 12 ]] || { printf '%s\n' 'This acceptance runner requires Debian 12.' >&2; exit 1; }
case $(uname -m) in x86_64) arch=amd64;;aarch64|arm64) arch=arm64;;*) exit 1;;esac
[[ -n ${XRAY_BINARY:-} && $XRAY_BINARY == /* && -x $XRAY_BINARY ]] || { printf '%s\n' 'Provide XRAY_BINARY as an absolute path to a verified official Linux Xray executable.' >&2; exit 1; }
if command -v systemctl >/dev/null; then
    for service in xray.service zic-xray-agent.service; do
        if systemctl is-active --quiet "$service"; then printf '%s\n' 'Refusing: an existing Xray/agent service is active. Use a disposable, unused VM.' >&2; exit 1; fi
    done
fi
project_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
[[ -f "$project_root/dist/SHA256SUMS" && -d "$project_root/dist/lab-tests/linux-$arch" ]] || { printf '%s\n' 'Build test artifacts with -IncludeTests / --include-tests first.' >&2; exit 1; }
awk -v prefix="lab-tests/linux-$arch/" -v agent="linux-$arch/zic-xray-agent" 'index($2,prefix)==1 || $2==agent {print;found=1} END {if(!found)exit 1}' "$project_root/dist/SHA256SUMS" | (cd "$project_root/dist" && sha256sum --check --strict)
result_dir=$(mktemp -d "${TMPDIR:-/tmp}/zic-debian12-lab.XXXXXXXX")
trap 'printf "Lab results retained at %s\n" "$result_dir"' EXIT
unset ZIC_DAEMON_CRASH_HELPER ZIC_SEND_CRASH_HELPER
export XRAY_BINARY
"$XRAY_BINARY" version > "$result_dir/xray-version.txt"
mapfile -t test_files < <(awk -v prefix="lab-tests/linux-$arch/" 'index($2,prefix)==1 {print $2}' "$project_root/dist/SHA256SUMS")
[[ ${#test_files[@]} -gt 0 ]] || { printf '%s\n' 'No checked test binaries in manifest.' >&2; exit 1; }
for relative in "${test_files[@]}"; do
    [[ $relative =~ ^lab-tests/linux-(amd64|arm64)/[a-z0-9-]+\.test$ ]] || { printf '%s\n' 'Unsafe test manifest path.' >&2; exit 1; }
    test_binary="$project_root/dist/$relative"
    chmod 0700 -- "$test_binary"
    "$test_binary" -test.v -test.timeout=90s > "$result_dir/$(basename -- "$test_binary").log" 2>&1
done
# Parse/check the unit only. Rebind ExecStart to the already-built test binary;
# the verifier does not start it or read a real panel credential.
if command -v systemd-analyze >/dev/null; then
    awk -v binary="$project_root/dist/linux-$arch/zic-xray-agent" '{if($0~/^ExecStart=/)sub(/^ExecStart=[^ ]+/,"ExecStart=\"" binary "\""); print}' "$project_root/packaging/zic-xray-agent.service" > "$result_dir/zic-xray-agent.service"
    systemd-analyze verify "$result_dir/zic-xray-agent.service" > "$result_dir/systemd-verify.log" 2>&1
else
    printf '%s\n' 'systemd-analyze missing; unit verification NOT RUN.' > "$result_dir/systemd-verify.log"
    exit 1
fi
printf '%s\n' 'Functional tests passed on this VM. This does not establish production readiness or RSS/CPU budgets.'
