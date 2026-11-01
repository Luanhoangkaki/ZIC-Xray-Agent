#!/bin/bash
# Four choices + three fields, no other interactive questions. Observe only.
set +x
set +v
set -Eeuo pipefail
umask 077
unset server_token
readonly release_version='v1.0.1-audit3-test'
if [[ ${1:-} == --help ]]; then
    printf 'ZIC-Xray-Agent %s\n' "$release_version"
    printf '%s\n' 'Usage: sudo bash install.sh' 'Menu 1..4, then Panel domain, visible API Key, Node ID.' 'Only verified VLESS adapters can install; observe only, no POST or Xray changes.'
    exit 0
fi
[[ $# == 0 ]] || { printf '%s\n' 'Không nhận API Key hay cấu hình qua argv.' >&2; exit 1; }
[[ $EUID == 0 && $(uname -s) == Linux && -f /etc/os-release ]] || { printf '%s\n' 'Chạy installer bằng sudo trên Debian 12.' >&2; exit 1; }
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 12 ]] || { printf '%s\n' 'Gói này dành cho Debian 12.' >&2; exit 1; }
case $(uname -m) in x86_64) arch=amd64;;aarch64|arm64) arch=arm64;;*) exit 1;;esac
for tool in systemctl systemd-analyze flock sha256sum install realpath; do command -v "$tool" >/dev/null; done
script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
(cd "$script_dir" && sha256sum --check --strict SHA256SUMS >/dev/null)
unit=zic-xray-agent.service
unit_path=/etc/systemd/system/zic-xray-agent.service
config_dir=/etc/zic-xray-agent
binary_dir=/usr/local/lib/zic-xray-agent
receipt_root=/var/lib/zic-xray-agent-installer
exec 9>/run/lock/zic-xray-agent-install.lock
flock -n 9 || { printf '%s\n' 'Một lần cài Agent khác đang chạy.' >&2; exit 1; }
for path in "$unit_path" "$config_dir" "$binary_dir" /var/lib/zic-xray-agent /var/lib/private/zic-xray-agent /var/log/zic-xray-agent /var/log/private/zic-xray-agent /etc/systemd/system/zic-xray-agent.service.d; do
    [[ ! -e $path && ! -L $path ]] || { printf '%s\n' 'Agent/config/state đã tồn tại; từ chối ghi đè. Lab không bị thay đổi.' >&2; exit 1; }
done
[[ $(systemctl show --property LoadState --value "$unit") == not-found ]] || { printf '%s\n' 'Tên service Agent đang được sử dụng; chưa cài.' >&2; exit 1; }
[[ -z $(systemctl show --property DropInPaths --value "$unit") ]] || exit 1
exec 3<>/dev/tty
printf '%s\n' '1. Shadowsocks' '2. Trojan' '3. Hysteria' '4. VLESS' >&3
printf '%s' 'Nhập lựa chọn [1-4]: ' >&3
IFS= read -r selection <&3
[[ $selection =~ ^[1-4]$ ]] || { printf '%s\n' 'Lựa chọn không hợp lệ.' >&2; exit 1; }
printf '%s' 'Nhập tên miền Panel: ' >&3
IFS= read -r panel_url <&3
printf '%s' 'Nhập API Key Panel: ' >&3
# User-requested visible TTY input. Never repeat this value in app output.
IFS= read -r server_token <&3
printf '%s' 'Nhập Node ID: ' >&3
IFS= read -r node_id <&3
exec 3>&-
# Strict decimal int64; no signs, zero, exponent, whitespace or leading zero.
[[ $node_id =~ ^[1-9][0-9]{0,18}$ && ( ${#node_id} -lt 19 || $node_id < 9223372036854775808 ) ]] || { unset server_token; printf '%s\n' 'Node ID phải là số nguyên dương trong phạm vi int64.' >&2; exit 1; }
[[ ! -L $receipt_root && ( ! -e $receipt_root || -d $receipt_root ) ]] || exit 1
install -d -m 0700 -- "$receipt_root"
transaction=$(mktemp -d "$receipt_root/install.XXXXXXXX")
install -d -m 0700 -- "$transaction/config"
install -m 0700 -- "$script_dir/rollback-install.sh" "$transaction/rollback.sh"
rollback_on_error() {
    local status=$?
    trap - EXIT INT TERM
    unset server_token
    if [[ $status != 0 ]]; then
        printf 'Cài đặt thất bại; evidence riêng: %s\n' "$transaction" >&2
        # Retain our transaction lock while the rollback child runs. It does
        # not acquire a second lock; the public rollback entrypoint does.
        if ! bash "$transaction/rollback.sh" --owned-transaction "$transaction"; then
            printf '%s\n' 'Rollback chưa hoàn tất; giữ evidence và kiểm tra service Agent. Lab không bị thay đổi.' >&2
        fi
    fi
    exit "$status"
}
trap rollback_on_error EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
install -m 0700 -- "$script_dir/bin/linux-$arch/zic-xray-agent" "$transaction/agent"
expected=$(awk -v target="bin/linux-$arch/zic-xray-agent" '{sub(/^\*/,"",$2); if($2 == target)print $1}' "$script_dir/SHA256SUMS")
actual=$(sha256sum "$transaction/agent")
[[ $expected =~ ^[a-f0-9]{64}$ && ${actual%% *} == "$expected" ]] || exit 1
xray_binary=${XRAY_BINARY:-"$script_dir/validation/linux-$arch/xray"}
if [[ -n $xray_binary ]]; then
    xray_binary=$(realpath -e -- "$xray_binary" 2>/dev/null) || { printf '%s\n' 'E_INSTALL_CORE_CORE_BINARY' >&2; exit 1; }
fi
# Bash builtin printf: the secret crosses stdin, never argv or exported env.
printf '%s\0%s\0%s\0' "$panel_url" "$server_token" "$node_id" | "$transaction/agent" configure-install --selection "$selection" --xray-binary "$xray_binary" --output-dir "$transaction/config"
unset server_token
# No machine changes until authentication, node, policy and offline core
# validation have all passed. Only fresh installation is authorized here.
mkdir -m 0755 -- "$binary_dir"
: > "$transaction/owned-binary"
install -m 0755 -- "$transaction/agent" "$binary_dir/zic-xray-agent"
mv -T -n -- "$transaction/config" "$config_dir"
[[ ! -d $transaction/config ]] || exit 1
: > "$transaction/owned-config"
install -m 0644 -- "$script_dir/packaging/zic-xray-agent.service" "$transaction/unit.service"
mv -T -n -- "$transaction/unit.service" "$unit_path"
[[ ! -e $transaction/unit.service ]] || exit 1
: > "$transaction/owned-unit"
sha256sum "$binary_dir/zic-xray-agent" "$config_dir/config.json" "$config_dir/panel-config.json" "$unit_path" > "$transaction/deployed.sha256"
systemd-analyze verify "$unit_path"
systemctl daemon-reload
systemctl enable --now "$unit"
ready=0
for ((attempt=0; attempt<45; attempt++)); do
    if systemctl is-active --quiet "$unit" && "$binary_dir/zic-xray-agent" install-status --config "$config_dir/config.json" --state-dir /var/lib/zic-xray-agent/data >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
done
[[ $ready == 1 ]] || { printf '%s\n' 'Agent chưa có snapshot xác thực sau khởi động.' >&2; exit 1; }
printf '%s\n' 'Đã cài và khởi động ZIC-Xray-Agent observe. Không bật proxy, không gửi traffic POST, không sửa hoặc restart Xray/Lab.'
printf 'Rollback: sudo bash %s/rollback.sh rollback %s\n' "$transaction" "$transaction"
