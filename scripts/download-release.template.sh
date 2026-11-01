#!/bin/bash
# Generated for one reviewed release. Default: verify and extract, never install.
set -euo pipefail
umask 077
readonly version='v1.0.1'
readonly release_tag='@RELEASE_TAG@'
readonly repository='Luanhoangkaki/ZIC-Xray-Agent'
readonly sha_amd64='@SHA256_AMD64@'
readonly sha_arm64='@SHA256_ARM64@'
mode=${1:---extract-only}
[[ $# -le 1 && ( $mode == --extract-only || $mode == --install || $mode == --help ) ]] || { printf '%s\n' 'Usage: bash get-zic-xray-agent-v1.0.1.sh [--extract-only|--install]' >&2; exit 1; }
if [[ $mode == --help ]]; then
    printf '%s\n' 'TEST ONLY / OBSERVE MODE / NOT PRODUCTION READY. Fixed candidate tag; embedded SHA256 pins.' 'Default --extract-only verifies before listing/extracting; --install explicitly launches the verified installer on an unused test machine.'
    exit 0
fi
[[ $(uname -s) == Linux && -f /etc/os-release ]] || { printf '%s\n' 'Requires Debian 12 Linux.' >&2; exit 1; }
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_ID:-} == 12 ]] || { printf '%s\n' 'Requires Debian 12.' >&2; exit 1; }
case $(uname -m) in
    x86_64) arch=amd64; expected=$sha_amd64;;
    aarch64|arm64) arch=arm64; expected=$sha_arm64;;
    *) printf '%s\n' 'Unsupported architecture.' >&2; exit 1;;
esac
for tool in curl sha256sum tar awk mktemp chmod; do command -v "$tool" >/dev/null; done
if [[ $mode == --install ]]; then
    command -v systemctl >/dev/null
    # Never automatically install on the known active lab or an active node.
    # This is a guard, not a claim that every production service is detectable.
    for unit in zic-lab-panel.service zic-lab-core.service zic-lab-agent.service zic-xray-agent.service xray.service; do
        if systemctl is-active --quiet "$unit"; then
            printf '%s\n' 'Refusing --install: a lab/agent/Xray service is active. Use --extract-only and review on an unused test machine.' >&2
            exit 1
        fi
    done
fi
name="zic-xray-agent-installer-debian12-$arch-$version"
asset="$name.tar.gz"
base="https://github.com/$repository/releases/download/$release_tag"
work=$(mktemp -d "${TMPDIR:-/tmp}/zic-xray-v1.0.1.XXXXXXXX")
trap 'printf "Download evidence retained: %s\n" "$work"' EXIT
download() {
    # Disable .curlrc; bound time, retries and accepted protocols. No auth data.
    curl --disable --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 300 --retry 2 --retry-delay 2 --max-filesize "$3" --output "$2" "$1"
}
download "$base/$asset.sha256" "$work/$asset.sha256" 512
mapfile -t sums < "$work/$asset.sha256"
[[ ${#sums[@]} == 1 && ${sums[0]} == "$expected  $asset" ]] || { printf '%s\n' 'Release sidecar does not match the embedded SHA256 pin. Nothing extracted or executed.' >&2; exit 1; }
download "$base/$asset" "$work/$asset" 134217728
(cd "$work" && printf '%s  %s\n' "$expected" "$asset" | sha256sum --check --strict)
# No tar operation occurs before the checksum above succeeds.
tar -tzf "$work/$asset" > "$work/members.txt"
count=0
while IFS= read -r member; do
    [[ $member == "$name/"* && $member =~ ^[a-zA-Z0-9_./-]+$ && /$member/ != */../* && /$member/ != */./* ]] || { printf '%s\n' 'Unsafe archive path; refusing extraction.' >&2; exit 1; }
    count=$((count+1)); [[ $count -le 256 ]] || exit 1
done < "$work/members.txt"
[[ $count -gt 0 ]] || exit 1
# Refuse symlinks, hardlinks, devices and unexpectedly large expanded content.
LC_ALL=C tar -tvzf "$work/$asset" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} $3 !~ /^[0-9]+$/ {bad=1} {total+=$3} END {exit (bad || total>268435456)}'
tar --extract --gzip --file "$work/$asset" --directory "$work" --no-same-owner --no-same-permissions
(cd "$work/$name" && sha256sum --check --strict SHA256SUMS >/dev/null)
# Only after the pinned archive and inner checksums have passed. Set both
# executable modes explicitly so restrictive extraction masks cannot strip x.
chmod 0700 -- "$work/$name/bin/linux-$arch/zic-xray-agent" "$work/$name/validation/linux-$arch/xray" 2>/dev/null || { printf '%s\n' 'E_DOWNLOAD_EXEC_MODE' >&2; exit 1; }
[[ -x $work/$name/bin/linux-$arch/zic-xray-agent && -x $work/$name/validation/linux-$arch/xray ]] || { printf '%s\n' 'E_DOWNLOAD_EXEC_MODE' >&2; exit 1; }
printf 'Verified TEST ONLY installer (%s): %s\n' "$release_tag" "$work/$name/install.sh"
if [[ $mode == --install ]]; then
    if [[ $EUID == 0 ]]; then bash "$work/$name/install.sh"; else sudo bash "$work/$name/install.sh"; fi
fi
