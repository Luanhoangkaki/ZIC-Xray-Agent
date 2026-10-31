#!/bin/bash
set +x
set +v
set -euo pipefail
umask 077
[[ $# == 2 && $EUID == 0 && ( $1 == rollback || $1 == --owned-transaction ) ]] || exit 1
action=$1
receipt_root=/var/lib/zic-xray-agent-installer
transaction=$(realpath -e -- "$2")
[[ $(dirname -- "$transaction") == "$receipt_root" && ${transaction##*/} =~ ^install\.[A-Za-z0-9]+$ && ! -L $transaction ]] || exit 1
if [[ $action == rollback ]]; then
    exec 9>/run/lock/zic-xray-agent-install.lock
    flock -n 9 || { printf '%s\n' 'Installer đang chạy; chưa rollback.' >&2; exit 1; }
else
    # The automatic rollback must inherit the installer's existing lock FD.
    [[ -e /proc/self/fd/9 ]] || exit 1
fi
[[ ! -e $transaction/rolled-back ]] || { printf '%s\n' 'Transaction đã được rollback.'; exit 0; }
unit=zic-xray-agent.service
unit_path=/etc/systemd/system/zic-xray-agent.service
config_dir=/etc/zic-xray-agent
binary_dir=/usr/local/lib/zic-xray-agent
for path in "$unit_path" "$config_dir" "$binary_dir"; do [[ ! -L $path ]] || exit 1; done
if [[ -f $transaction/deployed.sha256 ]]; then
    sha256sum --check --strict "$transaction/deployed.sha256" >/dev/null || { printf '%s\n' 'File đã sửa sau cài đặt; từ chối rollback trước khi thay đổi service.' >&2; exit 1; }
fi
if [[ -f $transaction/owned-unit ]]; then
    [[ -z $(systemctl show --property DropInPaths --value "$unit") ]] || { printf '%s\n' 'Có drop-in mới; cần review trước rollback.' >&2; exit 1; }
    systemctl stop "$unit"
    systemctl disable "$unit"
fi
install -d -m 0700 -- "$transaction/removed"
# Only paths created by this fresh installation are moved, never deleted.
# Config/credentials remain private; no state/log/ledger is rewound or removed.
if [[ -f $transaction/owned-unit && -e $unit_path ]]; then mv -T -- "$unit_path" "$transaction/removed/unit.service"; fi
if [[ -f $transaction/owned-config && -e $config_dir ]]; then mv -T -- "$config_dir" "$transaction/removed/config"; fi
if [[ -f $transaction/owned-binary && -e $binary_dir ]]; then mv -T -- "$binary_dir" "$transaction/removed/binary"; fi
if [[ -f $transaction/owned-unit ]]; then systemctl daemon-reload; fi
: > "$transaction/rolled-back"
printf '%s\n' 'Đã rollback các file/service Agent của lần cài mới; giữ config và evidence riêng. Không thay đổi Xray hoặc Lab.'
