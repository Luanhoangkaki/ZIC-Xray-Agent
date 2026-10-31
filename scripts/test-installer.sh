#!/bin/bash
# Rootless installer/UI/rollback regression. All OS paths and service commands
# are replaced in private copies. Never calls real systemctl or /dev/tty.
set -euo pipefail
umask 077
project=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/zic-installer-test.XXXXXXXX")
host_os=$(uname -s)
trap 'printf "Installer mock evidence: %s\n" "$work"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
mkdir -p "$work/mock-bin"
cat > "$work/mock-bin/systemctl" <<'STUB'
#!/bin/bash
set -eu
printf '%s\n' "$*" >> "$CASE_ROOT/commands.log"
[[ $* != *zic-lab* && $* != *xray.service* ]] || exit 99
case $1 in
show)
    if [[ $3 == LoadState ]]; then printf '%s\n' not-found; fi
    ;;
daemon-reload|disable) :;;
enable)
    [[ ${FAIL_ENABLE:-0} == 0 ]] || exit 1
    : > "$CASE_ROOT/agent.active"
    ;;
stop) rm -f "$CASE_ROOT/agent.active";;
is-active) [[ -f $CASE_ROOT/agent.active ]];;
*) exit 1;;
esac
STUB
cat > "$work/mock-bin/systemd-analyze" <<'STUB'
#!/bin/sh
[ "${FAIL_UNIT:-0}" = 0 ]
STUB
cat > "$work/mock-bin/flock" <<'STUB'
#!/bin/sh
exit 0
STUB
cat > "$work/mock-bin/uname" <<'STUB'
#!/bin/sh
case $1 in -m) printf '%s\n' x86_64;;-s) printf '%s\n' Linux;;*) exit 1;;esac
STUB
cat > "$work/mock-bin/sleep" <<'STUB'
#!/bin/sh
# Serialize fast mock attempts; production installer retains real 1s waits.
exit 0
STUB
chmod 0700 "$work/mock-bin/"*
if [[ $host_os == MINGW* || $host_os == MSYS* ]]; then
    cat > "$work/mock-bin/install" <<'STUB'
#!/bin/sh
set -eu
if [ "$1" = -d ]; then shift 3; [ "${1:-}" != -- ] || shift; exec /usr/bin/mkdir -p "$@"; fi
shift 2; [ "${1:-}" != -- ] || shift
exec /usr/bin/cp "$@"
STUB
    cat > "$work/mock-bin/mkdir" <<'STUB'
#!/bin/sh
set -eu
if [ "${1:-}" = -m ]; then shift 2; fi
exec /usr/bin/mkdir "$@"
STUB
    chmod 0700 "$work/mock-bin/install" "$work/mock-bin/mkdir"
fi
export PATH="$work/mock-bin:$PATH"

setup_case() {
    export CASE_ROOT="$work/$1"
    mkdir -p "$CASE_ROOT/package/bin/linux-amd64" "$CASE_ROOT/package/validation/linux-amd64" "$CASE_ROOT/package/packaging" "$CASE_ROOT/etc/systemd/system" "$CASE_ROOT/var/lib" "$CASE_ROOT/var/log" "$CASE_ROOT/usr/local/lib" "$CASE_ROOT/run/lock" "$CASE_ROOT/opt/zic-xray-lab"
    printf 'ID=debian\nVERSION_ID=12\n' > "$CASE_ROOT/etc/os-release"
    printf '%s\n' 'never touch running lab' > "$CASE_ROOT/opt/zic-xray-lab/sentinel"
    printf '%s\n' 'never touch lab unit' > "$CASE_ROOT/etc/systemd/system/zic-lab-core.service"
    sha256sum "$CASE_ROOT/opt/zic-xray-lab/sentinel" "$CASE_ROOT/etc/systemd/system/zic-lab-core.service" > "$CASE_ROOT/lab.sha256"
    cp "$project/packaging/zic-xray-agent.service" "$CASE_ROOT/package/packaging/zic-xray-agent.service"
    # Only the test COPY substitutes fixed OS roots and root/TTY checks.
    for pair in 'install.sh:install.sh' 'packaging/rollback-agent-install.sh:rollback-install.sh'; do
        input=${pair%%:*}; output=${pair##*:}
        sed -e "s|/etc/|$CASE_ROOT/etc/|g" -e "s|/var/lib/|$CASE_ROOT/var/lib/|g" -e "s|/var/log/|$CASE_ROOT/var/log/|g" -e "s|/usr/local/lib/|$CASE_ROOT/usr/local/lib/|g" -e "s|/run/lock/|$CASE_ROOT/run/lock/|g" -e "s|/dev/tty|$CASE_ROOT/input|g" -e 's/$EUID == 0/1 == 1/g' -e 's/>&3/>\&2/g' "$project/$input" > "$CASE_ROOT/package/$output"
    done
    cat > "$CASE_ROOT/package/bin/linux-amd64/zic-xray-agent" <<'STUB'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$CASE_ROOT/agent-args.log"
case $1 in
configure-install)
    [[ ! -v server_token ]] || exit 1
    choice=$3; output=${@: -1}
    IFS= read -r -d '' panel
    IFS= read -r -d '' secret
    IFS= read -r -d '' node
    [[ $choice == 4 && ${FAIL_PREFLIGHT:-0} == 0 ]] || exit 1
    printf '{"mode":"observe","server_token":"%s"}\n' "$secret" > "$output/config.json"
    printf '%s\n' '{"network":"tcp"}' > "$output/panel-config.json"
    ;;
install-status) [[ ${FAIL_STATUS:-0} == 0 ]];;
*) exit 1;;
esac
STUB
    chmod 0700 "$CASE_ROOT/package/bin/linux-amd64/zic-xray-agent"
    printf '%s\n' 'mock validator, never executed by the transaction/UI test' > "$CASE_ROOT/package/validation/linux-amd64/xray"
    (cd "$CASE_ROOT/package" && sha256sum install.sh rollback-install.sh bin/linux-amd64/zic-xray-agent validation/linux-amd64/xray packaging/zic-xray-agent.service > SHA256SUMS)
    printf '%s\n' "${2:-4}" 127.0.0.1 'FAKE-API-KEY-not-production' "${3:-42}" > "$CASE_ROOT/input"
}
run_installer() { server_token=parent-export-placeholder bash -x "$CASE_ROOT/package/install.sh" > "$CASE_ROOT/stdout" 2> "$CASE_ROOT/stderr"; }
assert_safe() {
    for log in stdout stderr commands.log agent-args.log; do
        if [[ -f $CASE_ROOT/$log ]] && grep -q 'FAKE-API-KEY-not-production' "$CASE_ROOT/$log"; then fail "key leaked in $log"; fi
    done
    sha256sum --check --strict "$CASE_ROOT/lab.sha256" >/dev/null || fail 'Lab changed'
}
assert_no_install() {
    [[ ! -e $CASE_ROOT/etc/zic-xray-agent && ! -e $CASE_ROOT/usr/local/lib/zic-xray-agent && ! -e $CASE_ROOT/etc/systemd/system/zic-xray-agent.service && ! -e $CASE_ROOT/agent.active ]] || fail 'failed install left active/public agent files'
    assert_safe
}

setup_case valid
run_installer
assert_safe
[[ $(grep -o 'Nhập ' "$CASE_ROOT/stderr" | wc -l) == 4 ]] || fail 'wrong prompt count'
for label in '1. Shadowsocks' '2. Trojan' '3. Hysteria' '4. VLESS'; do grep -Fq "$label" "$CASE_ROOT/stderr" || fail 'wrong menu'; done
grep -Fq 'Nhập lựa chọn [1-4]: Nhập tên miền Panel: Nhập API Key:' "$CASE_ROOT/stderr" || fail 'wrong prompt order'
[[ -f $CASE_ROOT/agent.active ]] || fail 'Agent was not started'
transaction=$(find "$CASE_ROOT/var/lib/zic-xray-agent-installer" -maxdepth 1 -type d -name 'install.*')
bash "$transaction/rollback.sh" rollback "$transaction" >> "$CASE_ROOT/stdout" 2>> "$CASE_ROOT/stderr"
assert_no_install
[[ -f $transaction/removed/config/config.json ]] || fail 'rollback did not retain credentials privately'
printf '%s\n' 'PASS exact menu/field order; hidden-read flag; no key in xtrace/output/argv; autostart and manual rollback; Lab untouched'
grep -q 'read -r -s server_token' "$project/install.sh" || fail 'API Key echo not disabled'

for choice in 1 2 3 0 5 '04' '4 '; do
    setup_case "choice-${choice// /space}" "$choice"
    if run_installer; then fail "unsupported/invalid choice accepted: $choice"; fi
    assert_no_install
done
printf '%s\n' 'PASS unsupported and invalid protocol choices do not install'

for node in 0 -1 abc 01 9223372036854775808 999999999999999999999999; do
    setup_case "node-$node" 4 "$node"
    if run_installer; then fail "invalid node accepted: $node"; fi
    assert_no_install
done
setup_case max-node 4 9223372036854775807
run_installer
assert_safe
printf '%s\n' 'PASS Node ID strict positive int64 validation and upper bound'

for failure in FAIL_PREFLIGHT FAIL_UNIT FAIL_ENABLE FAIL_STATUS; do
    setup_case "$failure"
    if env "$failure=1" bash "$CASE_ROOT/package/install.sh" > "$CASE_ROOT/stdout" 2> "$CASE_ROOT/stderr"; then fail "fault accepted: $failure"; fi
    assert_no_install
done
printf '%s\n' 'PASS preflight/unit/start/snapshot faults automatically rollback'

setup_case existing
mkdir -p "$CASE_ROOT/etc/zic-xray-agent"
printf '%s\n' preserve-existing > "$CASE_ROOT/etc/zic-xray-agent/sentinel"
if run_installer; then fail 'existing config overwritten'; fi
[[ $(cat "$CASE_ROOT/etc/zic-xray-agent/sentinel") == preserve-existing ]] || fail 'existing config changed'
assert_safe

setup_case changed-after-install
run_installer
transaction=$(find "$CASE_ROOT/var/lib/zic-xray-agent-installer" -maxdepth 1 -type d -name 'install.*')
printf '\n' >> "$CASE_ROOT/etc/zic-xray-agent/config.json"
if bash "$transaction/rollback.sh" rollback "$transaction" >> "$CASE_ROOT/stdout" 2>> "$CASE_ROOT/stderr"; then fail 'rollback overwrote changed config'; fi
[[ -f $CASE_ROOT/agent.active ]] || fail 'refused rollback stopped Agent'
assert_safe
printf '%s\n' 'PASS existing installation refused; rollback refuses later edits'
printf '%s\n' 'Installer mock regressions PASS. Real Debian/systemd/TTY permissions NOT RUN.'
