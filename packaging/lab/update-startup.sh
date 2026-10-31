#!/bin/bash
# Transactional repair for an EXISTING synthetic lab. Never replace binaries,
# credentials, Xray JSON, agent unit, state or ledger. No network downloads.
set -Eeuo pipefail
umask 077
lab_root=/opt/zic-xray-lab
action=${1:-help}
if [[ $action == help || $action == --help ]]; then
    printf '%s\n' 'sudo env ZIC_LAB=1 bash update-startup.sh check' 'sudo env ZIC_LAB=1 bash update-startup.sh apply' 'sudo env ZIC_LAB=1 bash BACKUP/rollback.sh rollback BACKUP'
    exit 0
fi
shift
[[ $EUID == 0 && ${ZIC_LAB:-} == 1 ]] || { printf '%s\n' 'Requires sudo and explicit ZIC_LAB=1 for the isolated lab.' >&2; exit 1; }
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 12 && $(uname -m) == x86_64 ]] || exit 1
[[ -d $lab_root && ! -L $lab_root && -f $lab_root/config/lab.json ]] || exit 1
exec 9>/run/lock/zic-xray-lab.lock
flock -n 9 || { printf '%s\n' 'Another lab operation is running.' >&2; exit 1; }
source_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
units=(zic-lab-panel.service zic-lab-core.service zic-lab-agent.service)
# Fixed destination allowlist. No config, binary, state, logs or PID backup.
destinations=("$lab_root/start.sh" "$lab_root/lab.sh" "$lab_root/units/zic-lab-panel.service" "$lab_root/units/zic-lab-core.service" /etc/systemd/system/zic-lab-panel.service /etc/systemd/system/zic-lab-core.service)
backup=

atomic_install() {
    local input=$1 output=$2 mode=$3
    install -m "$mode" -- "$input" "$output.startup-new" || return 1
    mv -f -- "$output.startup-new" "$output" || return 1
}
check_destinations() {
    local destination allow_temp=${1:-no}
    for destination in "${destinations[@]}"; do
        [[ ! -L $destination && ( ! -e $destination || -f $destination ) && ! -L $destination.startup-new ]] || return 1
        [[ ! -e $destination.startup-new || ( $allow_temp == yes && -f $destination.startup-new ) ]] || return 1
    done
}
validate_backup() {
    local unit
    [[ -d $backup && ! -L $backup && -f $backup/complete ]] || return 1
    (cd "$backup" && sha256sum --check --strict SHA256SUMS) || return 1
    # A manual rollback must not overwrite unreviewed edits made after apply.
    if [[ -f $backup/applied && $action == rollback ]]; then
        sha256sum --check --strict "$backup/deployed.sha256" || return 1
    fi
    check_destinations yes || return 1
    for unit in "${units[@]}"; do
        [[ -z $(systemctl show --property DropInPaths --value "$unit") ]] || return 1
    done
}
restore_files() {
    local i destination pid
    validate_backup || return 1
    systemctl stop zic-lab-agent.service zic-lab-core.service zic-lab-panel.service || return 1
    for i in "${!destinations[@]}"; do
        destination=${destinations[$i]}
        rm -f -- "$destination.startup-new" || return 1
        if [[ -f $backup/files/$i ]]; then
            cp -p -- "$backup/files/$i" "$destination.startup-new" || return 1
            mv -f -- "$destination.startup-new" "$destination" || return 1
        elif [[ -f $backup/files/$i.absent ]]; then
            # Only a fixed allowlisted file that was absent before the repair.
            rm -f -- "$destination" || return 1
        else
            return 1
        fi
    done
    systemctl daemon-reload || return 1
    systemctl start zic-lab-panel.service zic-lab-core.service || return 1
    # The previous workaround may have removed ALL ExecStartPost commands.
    # Never restore a stale PID: publish the new verified running core PID.
    pid=$(systemctl show --property MainPID --value zic-lab-core.service) || return 1
    [[ $pid =~ ^[1-9][0-9]*$ && $(readlink "/proc/$pid/exe") == "$lab_root/bin/xray" ]] || return 1
    "$lab_root/bin/zic-lab" write-pid --pid "$pid" --out /run/zic-lab-core/xray.pid || return 1
    systemctl start zic-lab-agent.service || return 1
    "$lab_root/bin/zic-lab" ready --config "$lab_root/config/lab.json" || return 1
    for destination in "${units[@]}"; do systemctl is-active --quiet "$destination" || return 1; done
    printf '%s\n' 'Previous lab startup files restored; fresh PID published. State/ledger untouched.'
}
failed_apply() {
    local status=$1
    trap - ERR INT TERM
    printf 'Startup repair failed; attempting rollback from %s\n' "$backup" >&2
    if restore_files; then
        printf '%s\n' 'Rollback completed. The repair is NOT a PASS.' >&2
    else
        # Fail closed if the old workaround no longer starts. Do not pretend
        # rollback succeeded or erase evidence/counters to recover service.
        systemctl stop zic-lab-agent.service zic-lab-core.service zic-lab-panel.service || true
        printf 'Rollback failed; lab services stopped. Preserve %s and inspect lab journal.\n' "$backup" >&2
    fi
    exit "$status"
}

case $action in
check|apply)
    [[ $# == 0 && -x /usr/bin/systemd-notify ]] || exit 1
    (cd "$source_dir" && sha256sum --check --strict SHA256SUMS)
    check_destinations
    for unit in "${units[@]}"; do
        [[ $(systemctl show --property FragmentPath --value "$unit") == "/etc/systemd/system/$unit" ]] || { printf '%s\n' 'Unexpected lab unit origin; review before repairing.' >&2; exit 1; }
        [[ -z $(systemctl show --property DropInPaths --value "$unit") ]] || { printf '%s\n' 'Lab drop-ins exist. Back up and review/remove ONLY the temporary lab workaround before applying.' >&2; exit 1; }
        systemctl is-active --quiet "$unit" || { printf '%s\n' 'Repair expects the three currently working lab services.' >&2; exit 1; }
    done
    # Validate local configuration through the existing loopback-only lab tool.
    "$lab_root/bin/zic-lab" ready --config "$lab_root/config/lab.json"
    systemd-analyze verify "$source_dir/units/zic-lab-panel.service" "$source_dir/units/zic-lab-core.service"
    for unit in zic-lab-panel.service zic-lab-core.service; do
        # A diff exit code of 1 means differences, not failure. No secrets here.
        diff_status=0
        diff -u -- "/etc/systemd/system/$unit" "$source_dir/units/$unit" || diff_status=$?
        [[ $diff_status -le 1 ]] || exit 1
    done
    if [[ $action == check ]]; then
        printf '%s\n' 'Preflight/diff only; installed files and services were not changed.'
        exit 0
    fi
    backup=$(mktemp -d "$lab_root/results/startup-fix.backup.XXXXXXXX")
    install -d -m 0700 -- "$backup/files"
    for i in "${!destinations[@]}"; do
        if [[ -e ${destinations[$i]} ]]; then cp -p -- "${destinations[$i]}" "$backup/files/$i"; else : > "$backup/files/$i.absent"; fi
    done
    cp -- "${BASH_SOURCE[0]}" "$backup/rollback.sh"
    systemctl show "${units[@]}" --property Id,MainPID,ActiveState,UnitFileState,Type,ExecStartPost > "$backup/services-before.txt"
    printf '%s\n' 'Only startup files; no state or PID rollback.' > "$backup/complete"
    (cd "$backup" && sha256sum files/* rollback.sh services-before.txt complete > SHA256SUMS)
    printf 'Backup/rollback directory: %s\n' "$backup"
    trap 'failed_apply $?' ERR
    trap 'failed_apply 130' INT
    trap 'failed_apply 143' TERM
    systemctl stop zic-lab-agent.service zic-lab-core.service zic-lab-panel.service
    atomic_install "$source_dir/start.sh" "$lab_root/start.sh" 0755
    atomic_install "$source_dir/lab.sh" "$lab_root/lab.sh" 0755
    for unit in zic-lab-panel.service zic-lab-core.service; do
        atomic_install "$source_dir/units/$unit" "$lab_root/units/$unit" 0644
        atomic_install "$source_dir/units/$unit" "/etc/systemd/system/$unit" 0644
    done
    sha256sum -- "${destinations[@]}" > "$backup/deployed.sha256"
    systemctl daemon-reload
    systemctl start zic-lab-panel.service zic-lab-core.service zic-lab-agent.service
    # Keep the operation lock held; perform the startup checks inline rather
    # than invoking lab.sh, which deliberately takes the same lock.
    for unit in "${units[@]}"; do systemctl is-active --quiet "$unit"; done
    for unit in zic-lab-panel.service zic-lab-core.service; do
        [[ $(systemctl show --property Type --value "$unit") == notify ]]
        [[ -z $(systemctl show --property ExecStartPost --value "$unit") ]]
    done
    pid=$(systemctl show --property MainPID --value zic-lab-core.service)
    [[ $pid =~ ^[1-9][0-9]*$ && $(cat /run/zic-lab-core/xray.pid) == "$pid" && $(readlink "/proc/$pid/exe") == "$lab_root/bin/xray" ]]
    "$lab_root/bin/zic-lab" ready --config "$lab_root/config/lab.json"
    systemctl show "${units[@]}" --property Id,MainPID,ActiveState,Type,ExecStartPost > "$backup/services-after.txt"
    : > "$backup/applied"
    trap - ERR INT TERM
    printf 'Startup repair passed. Rollback: sudo env ZIC_LAB=1 bash %s/rollback.sh rollback %s\n' "$backup" "$backup"
    ;;
rollback)
    [[ $# == 1 ]] || exit 1
    backup=$(realpath -e -- "$1")
    [[ $backup == "$lab_root"/results/startup-fix.backup.* && ${backup##*/} =~ ^startup-fix\.backup\.[A-Za-z0-9]+$ ]] || exit 1
    validate_backup || { printf '%s\n' 'Rollback refused before mutation: backup/current files/drop-ins require review.' >&2; exit 1; }
    if ! restore_files; then
        printf '%s\n' 'Rollback failed; lab services stopped. Preserve the backup and inspect the lab journal.' >&2
        systemctl stop zic-lab-agent.service zic-lab-core.service zic-lab-panel.service || true
        exit 1
    fi
    ;;
*) exit 1;;
esac
