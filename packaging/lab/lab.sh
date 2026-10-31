#!/bin/bash
# Installs ONLY this self-contained lab. No production panel/credentials/core
# configuration is read. Every systemctl mutation names a zic-lab-* unit.
set -euo pipefail
umask 077
lab_root=/opt/zic-xray-lab
action=${1:-help}
if [[ $action == help || $action == --help ]]; then
    printf '%s\n' 'sudo env ZIC_LAB=1 bash lab.sh install [--base-port PORT]' 'sudo bash /opt/zic-xray-lab/lab.sh startup-check' 'sudo bash /opt/zic-xray-lab/lab.sh test [--quick]' 'sudo bash /opt/zic-xray-lab/lab.sh restart-check' 'sudo bash /opt/zic-xray-lab/lab.sh core-restart-check' 'sudo bash /opt/zic-xray-lab/lab.sh panel-restart-check' 'sudo bash /opt/zic-xray-lab/lab.sh prepare-reboot' 'sudo reboot  # only on a dedicated lab VPS, with operator approval' 'sudo bash /opt/zic-xray-lab/lab.sh verify-reboot' 'sudo bash /opt/zic-xray-lab/lab.sh stop'
    exit 0
fi
shift
[[ $EUID == 0 ]] || { printf '%s\n' 'Use sudo on the approved Debian 12 lab VPS.' >&2; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 && -f /etc/os-release ]] || exit 1
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 12 ]] || { printf '%s\n' 'This bundle targets Debian 12 amd64.' >&2; exit 1; }
command -v systemctl >/dev/null
command -v flock >/dev/null
exec 9>/run/lock/zic-xray-lab.lock
flock -n 9 || { printf '%s\n' 'Another lab operation is running.' >&2; exit 1; }
source_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
units=(zic-lab-panel.service zic-lab-core.service zic-lab-agent.service)

agent_pid() { systemctl show --property MainPID --value zic-lab-agent.service; }
core_pid() { systemctl show --property MainPID --value zic-lab-core.service; }
snapshot() { "$lab_root/bin/zic-lab" snapshot --config "$lab_root/config/lab.json" --agent-pid "$(agent_pid)" --out "$1"; }
ready() { "$lab_root/bin/zic-lab" ready --config "$lab_root/config/lab.json"; }
require_installed() { [[ -f "$lab_root/config/lab.json" && ! -L "$lab_root" ]] || { printf '%s\n' 'No initialized isolated lab.' >&2; exit 1; }; }
check_startup() {
    for unit in "${units[@]}"; do systemctl is-active --quiet "$unit" || return 1; done
    for unit in zic-lab-panel.service zic-lab-core.service; do
        [[ $(systemctl show --property Type --value "$unit") == notify ]] || return 1
        [[ -z $(systemctl show --property ExecStartPost --value "$unit") ]] || return 1
    done
    local pid
    pid=$(core_pid)
    [[ $pid =~ ^[1-9][0-9]*$ && -f /run/zic-lab-core/xray.pid && ! -L /run/zic-lab-core/xray.pid ]] || return 1
    [[ $(cat /run/zic-lab-core/xray.pid) == "$pid" ]] || return 1
    [[ $(readlink "/proc/$pid/exe") == "$lab_root/bin/xray" ]] || return 1
    ready
}

case $action in
install)
    [[ ${ZIC_LAB:-} == 1 ]] || { printf '%s\n' 'Refusing: install requires explicit ZIC_LAB=1 on an approved lab VPS.' >&2; exit 1; }
    [[ -x /usr/bin/systemd-notify ]] || { printf '%s\n' 'systemd-notify is required for startup readiness.' >&2; exit 1; }
    base_port=38080
    if [[ $# -eq 2 && $1 == --base-port && $2 =~ ^[0-9]+$ ]]; then base_port=$2; elif [[ $# -ne 0 ]]; then exit 1; fi
    [[ ! -e $lab_root && ! -L $lab_root ]] || { printf '%s\n' 'Lab root already exists; refusing overwrite.' >&2; exit 1; }
    for retained in /var/lib/zic-lab-agent /var/lib/private/zic-lab-agent /var/log/zic-lab-agent /var/log/private/zic-lab-agent; do
        [[ ! -e $retained && ! -L $retained ]] || { printf '%s\n' 'Old isolated lab state/logs exist; preserve them and review before reinstalling.' >&2; exit 1; }
    done
    for unit in "${units[@]}"; do
        [[ ! -e /etc/systemd/system/$unit && ! -L /etc/systemd/system/$unit ]] || { printf '%s\n' 'A lab unit already exists; refusing overwrite.' >&2; exit 1; }
        [[ $(systemctl show --property LoadState --value "$unit" 2>/dev/null) == not-found ]] || { printf '%s\n' 'A lab unit name is already in use.' >&2; exit 1; }
    done
    (cd "$source_dir" && sha256sum --check --strict SHA256SUMS)
    [[ $(find "$source_dir/tests" -maxdepth 1 -type f -name '*.test' | wc -l) == 11 ]] || { printf '%s\n' 'Expected exactly 11 amd64 test suites.' >&2; exit 1; }
    install -d -m 0755 -- "$lab_root" "$lab_root/bin" "$lab_root/units" "$lab_root/tests"
    install -d -m 0700 -- "$lab_root/results"
    for binary in zic-xray-agent zic-bench zic-lab xray; do install -m 0755 -- "$source_dir/bin/$binary" "$lab_root/bin/$binary"; done
    install -m 0755 -- "$source_dir/lab.sh" "$lab_root/lab.sh"
    install -m 0755 -- "$source_dir/start.sh" "$lab_root/start.sh"
    for test_binary in "$source_dir/tests/"*.test; do install -m 0755 -- "$test_binary" "$lab_root/tests/$(basename -- "$test_binary")"; done
    "$lab_root/bin/zic-lab" init --root "$lab_root" --base-port "$base_port"
    "$lab_root/bin/xray" run -test -config "$lab_root/config/xray.json" > "$lab_root/results/core-config-check.txt" 2>&1
    for unit in "${units[@]}"; do
        install -m 0644 -- "$source_dir/units/$unit" "$lab_root/units/$unit"
    done
    systemd-analyze verify "$lab_root/units/zic-lab-panel.service" "$lab_root/units/zic-lab-core.service" "$lab_root/units/zic-lab-agent.service"
    for unit in "${units[@]}"; do install -m 0644 -- "$lab_root/units/$unit" "/etc/systemd/system/$unit"; done
    # These unit names were verified absent above. If first activation fails,
    # disable/stop only the newly installed lab, retaining all files/evidence.
    trap 'status=$?; trap - ERR; systemctl disable --now zic-lab-agent.service zic-lab-core.service zic-lab-panel.service || true; printf "%s\n" "Lab activation failed; lab disabled/stopped, evidence retained under /opt/zic-xray-lab." >&2; exit "$status"' ERR
    systemctl daemon-reload
    systemctl enable --now "${units[@]}"
    check_startup
    snapshot "$lab_root/results/installed.json"
    trap - ERR
    printf '%s\n' 'Isolated observer lab installed. Traffic POST remains disabled. Existing Xray was not changed.'
    ;;
startup-check)
    require_installed;[[ $# == 0 ]] || exit 1
    check_startup
    printf '%s\n' 'Lab notify readiness and official core MainPID/PID file match.'
    ;;
test)
    require_installed
    quick=0
    if [[ $# -eq 1 && $1 == --quick ]]; then quick=1; elif [[ $# -ne 0 ]]; then exit 1; fi
    ready
    result_dir=$(mktemp -d "$lab_root/results/run.XXXXXXXX")
    trap 'printf "Results: %s\n" "$result_dir"' EXIT
    { cat /etc/os-release; uname -a; getconf _NPROCESSORS_ONLN; free -b; "$lab_root/bin/xray" version; } > "$result_dir/environment.txt"
    systemctl show "${units[@]}" --property Id,MainPID,NRestarts,ActiveState,MemoryCurrent,MemoryPeak,CPUUsageNSec > "$result_dir/services-before.txt"
    export XRAY_BINARY="$lab_root/bin/xray"
    unset ZIC_DAEMON_CRASH_HELPER ZIC_SEND_CRASH_HELPER
    # These suites use separate temporary loopback ports/configs and fake data.
    for test_binary in "$lab_root/tests/"*.test; do "$test_binary" -test.v -test.timeout=90s > "$result_dir/$(basename -- "$test_binary").log" 2>&1; done
    if [[ $quick == 0 ]]; then
        "$lab_root/bin/zic-lab" measure --config "$lab_root/config/lab.json" --agent-pid "$(agent_pid)" --core-pid "$(core_pid)" --connections 0 --duration 5m --out "$result_dir/warmup"
    fi
    for connections in 0 10 20 30; do
        duration=15m;[[ $connections == 0 ]] && duration=30m;[[ $quick == 1 ]] && duration=15s
        phase="$result_dir/connections-$connections"
        install -d -m 0700 -- "$phase"
        snapshot "$phase/ledger-before.json"
        "$lab_root/bin/zic-lab" measure --config "$lab_root/config/lab.json" --agent-pid "$(agent_pid)" --core-pid "$(core_pid)" --connections "$connections" --duration "$duration" --out "$phase"
        snapshot "$phase/ledger-after.json"
        "$lab_root/bin/zic-lab" check-phase --config "$lab_root/config/lab.json" --before "$phase/ledger-before.json" --after "$phase/ledger-after.json" --summary "$phase/summary.json" --out "$phase/accounting.json"
    done
    systemctl show "${units[@]}" --property Id,MainPID,NRestarts,ActiveState,MemoryCurrent,MemoryPeak,CPUUsageNSec > "$result_dir/services-after.txt"
    printf '%s\n' 'Lab checks passed. No production-readiness claim; read CSV/JSON evidence.'
    ;;
restart-check|core-restart-check|panel-restart-check)
    require_installed;[[ $# == 0 ]] || exit 1;ready
    result_dir=$(mktemp -d "$lab_root/results/restart.XXXXXXXX")
    snapshot "$result_dir/before.json"
    kind=restart
    if [[ $action == panel-restart-check ]]; then
        kind=core-restart
        systemctl restart zic-lab-panel.service zic-lab-core.service zic-lab-agent.service
    elif [[ $action == core-restart-check ]]; then
        kind=core-restart
        systemctl restart zic-lab-core.service zic-lab-agent.service
    else
        systemctl restart zic-lab-agent.service
    fi
    check_startup
    "$lab_root/bin/zic-lab" compare --config "$lab_root/config/lab.json" --agent-pid "$(agent_pid)" --before "$result_dir/before.json" --kind "$kind" --out "$result_dir/recovery.json"
    printf 'Restart result: %s\n' "$result_dir/recovery.json"
    ;;
prepare-reboot)
    require_installed;[[ $# == 0 ]] || exit 1;ready
    [[ ! -e "$lab_root/results/reboot-before.json" ]] || { printf '%s\n' 'Reboot marker exists; preserve/resolve it before a new test.' >&2; exit 1; }
    snapshot "$lab_root/results/reboot-before.json"
    printf '%s\n' 'Marker saved. No reboot was issued. Reboot only with operator approval, then run verify-reboot.'
    ;;
verify-reboot)
    require_installed;[[ $# == 0 ]] || exit 1;check_startup
    "$lab_root/bin/zic-lab" compare --config "$lab_root/config/lab.json" --agent-pid "$(agent_pid)" --before "$lab_root/results/reboot-before.json" --kind reboot --out "$lab_root/results/reboot-recovery.json"
    printf 'Reboot result: %s\n' "$lab_root/results/reboot-recovery.json"
    ;;
stop)
    require_installed;[[ $# == 0 ]] || exit 1
    systemctl disable --now zic-lab-agent.service zic-lab-core.service zic-lab-panel.service
    printf '%s\n' 'Only zic-lab-* stopped. State and results retained; existing Xray untouched.'
    ;;
*) printf '%s\n' 'Unknown lab action; use help.' >&2;exit 1;;
esac
