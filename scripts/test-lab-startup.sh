#!/bin/bash
# Rootless regression tests. ALL systemd/binaries/paths are replaced in private
# temporary copies. This is not a real systemd or Debian/VPS acceptance result.
set -euo pipefail
umask 077
project=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/zic-startup-test.XXXXXXXX")
host_os=$(uname -s)
trap 'printf "Mock regression evidence: %s\n" "$work"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# Launcher: credential access, no notify before ready, exec PID retention and
# new PID after two starts / simulated loss of the volatile runtime directory.
launch_root="$work/launcher"
mkdir -p "$launch_root/bin" "$launch_root/credentials" "$launch_root/run"
printf '%s\n' 'synthetic-only' > "$launch_root/credentials/lab.json"
for name in ca.pem cert.pem key.pem xray.json; do cp "$launch_root/credentials/lab.json" "$launch_root/credentials/$name"; done
cat > "$launch_root/bin/zic-lab" <<'STUB'
#!/bin/sh
set -eu
case $1 in
write-pid)
    [ "${FAIL_PID:-0}" = 0 ] || exit 1
    printf '%s\n' "$3" > "$5"
    ;;
ready)
    [ -r "$3" ] || exit 1
    [ "${FAIL_READY:-0}" = 0 ] || exit 1
    printf '%s\n' ready > "$TEST_ROOT/ready.ok"
    ;;
serve)
    [ -r "$3" ] || exit 1
    printf '%s\n' "$$" > "$TEST_ROOT/main.pid"
    sleep 0.2
    ;;
*) exit 1;;
esac
STUB
cat > "$launch_root/bin/xray" <<'STUB'
#!/bin/sh
set -eu
[ -r "$3" ] || exit 1
printf '%s\n' "$$" > "$TEST_ROOT/main.pid"
sleep 0.2
STUB
cat > "$launch_root/notify" <<'STUB'
#!/bin/sh
set -eu
[ -f "$TEST_ROOT/ready.ok" ] || exit 1
printf '%s\n' "$*" > "$TEST_ROOT/notify.args"
STUB
chmod 0700 "$launch_root/bin/"* "$launch_root/notify"
sed -e "s|/opt/zic-xray-lab|$launch_root|g" -e "s|/run/zic-lab-core|$launch_root/run|g" -e "s|/usr/bin/systemd-notify|$launch_root/notify|g" "$project/packaging/lab/start.sh" > "$launch_root/start.sh"
export TEST_ROOT="$launch_root" CREDENTIALS_DIRECTORY="$launch_root/credentials" NOTIFY_SOCKET="$launch_root/unused-mock-socket"
sh "$launch_root/start.sh" panel
[[ $(cat "$launch_root/notify.args") == "--ready --pid=$(cat "$launch_root/main.pid")" ]] || fail 'panel notify PID/readiness order'
previous=
for attempt in 1 2 3; do
    rm -f "$launch_root/main.pid" "$launch_root/notify.args" "$launch_root/ready.ok" "$launch_root/run/xray.pid"
    sh "$launch_root/start.sh" core
    pid=$(cat "$launch_root/main.pid")
    [[ $(cat "$launch_root/run/xray.pid") == "$pid" && $(cat "$launch_root/notify.args") == "--ready --pid=$pid" && $pid != "$previous" ]] || fail 'core PID not refreshed/retained by exec'
    previous=$pid
done
rm -f "$launch_root/notify.args" "$launch_root/ready.ok"
FAIL_READY=1 sh "$launch_root/start.sh" core
[[ ! -e $launch_root/notify.args ]] || fail 'failed readiness sent READY'
rm -f "$launch_root/main.pid" "$launch_root/notify.args"
if FAIL_PID=1 sh "$launch_root/start.sh" core; then fail 'PID write failure accepted'; fi
[[ ! -e $launch_root/main.pid && ! -e $launch_root/notify.args ]] || fail 'core started after PID write failure'
if CREDENTIALS_DIRECTORY= sh "$launch_root/start.sh" panel 2>/dev/null; then fail 'missing credentials accepted'; fi
printf '%s\n' 'PASS launcher: panel/core credential inheritance; notify ordering; exec PID; restart/volatile PID; readiness/PID failures'

# Transaction: filesystem + systemctl mocks. Tests never call real systemctl,
# write /opt or /etc, or bind any socket. Existing workaround is modelled by
# active units with ExecStartPost removed, just as in the reported incident.
case_root="$work/transaction"
mkdir -p "$case_root/mock-bin"
cat > "$case_root/mock-bin/systemctl" <<'STUB'
#!/bin/bash
set -eu
printf '%s\n' "$*" >> "$CASE_ROOT/commands.log"
action=$1; shift
case $action in
show)
    property=${2:-}; unit=${*: -1}
    case $property in
    FragmentPath) printf '%s/etc/%s\n' "$CASE_ROOT" "$unit";;
    DropInPaths) [[ ${FAIL_DROPIN:-0} == 0 ]] || printf '%s\n' /mock/workaround.conf;;
    Type) sed -n 's/^Type=//p' "$CASE_ROOT/etc/$unit";;
    ExecStartPost) sed -n 's/^ExecStartPost=//p' "$CASE_ROOT/etc/$unit";;
    MainPID) cat "$CASE_ROOT/core.pid";;
    *) printf '%s\n' mock-service-evidence;;
    esac
    ;;
is-active) [[ -f "$CASE_ROOT/${*: -1}.active" ]];;
stop) for unit in "$@"; do rm -f "$CASE_ROOT/$unit.active"; done;;
start)
    if [[ ${FAIL_NEW_START:-0} == 1 ]] && grep -q '^Type=notify$' "$CASE_ROOT/etc/zic-lab-core.service"; then exit 1; fi
    for unit in "$@"; do
        : > "$CASE_ROOT/$unit.active"
        if [[ $unit == zic-lab-core.service ]]; then
            pid=$(cat "$CASE_ROOT/core.pid"); printf '%s\n' "$((pid+1))" > "$CASE_ROOT/core.pid"
            if grep -q '^Type=notify$' "$CASE_ROOT/etc/$unit"; then cp "$CASE_ROOT/core.pid" "$CASE_ROOT/run/xray.pid"; fi
        fi
    done
    ;;
daemon-reload) :;;
*) exit 1;;
esac
STUB
cat > "$case_root/mock-bin/systemd-analyze" <<'STUB'
#!/bin/sh
exit 0
STUB
cat > "$case_root/mock-bin/flock" <<'STUB'
#!/bin/sh
# No concurrent transactions are started by this mock-only test.
exit 0
STUB
cat > "$case_root/mock-bin/readlink" <<'STUB'
#!/bin/sh
printf '%s\n' "$CASE_ROOT/lab/bin/xray"
STUB
cat > "$case_root/mock-bin/uname" <<'STUB'
#!/bin/sh
printf '%s\n' x86_64
STUB
chmod 0700 "$case_root/mock-bin/"*
if [[ $host_os == MINGW* || $host_os == MSYS* ]]; then
    # Git Bash cannot assert Linux ownership/modes. Test bytes/transactions
    # here, leaving real mode/ownership checks to Debian acceptance.
    cat > "$case_root/mock-bin/install" <<'STUB'
#!/bin/sh
set -eu
if [ "$1" = -d ]; then
    shift 3; [ "${1:-}" != -- ] || shift
    exec /usr/bin/mkdir -p "$@"
fi
shift 2; [ "${1:-}" != -- ] || shift
exec /usr/bin/cp "$@"
STUB
    cat > "$case_root/mock-bin/cp" <<'STUB'
#!/bin/sh
set -eu
[ "${1:-}" != -p ] || shift
exec /usr/bin/cp "$@"
STUB
    chmod 0700 "$case_root/mock-bin/install" "$case_root/mock-bin/cp"
fi
export PATH="$case_root/mock-bin:$PATH"

setup_transaction() {
    export CASE_ROOT="$case_root/$1"
    mkdir -p "$CASE_ROOT/source/units" "$CASE_ROOT/etc" "$CASE_ROOT/lab/units" "$CASE_ROOT/lab/config" "$CASE_ROOT/lab/bin" "$CASE_ROOT/lab/results" "$CASE_ROOT/run"
    printf 'ID=debian\nVERSION_ID=12\n' > "$CASE_ROOT/os-release"
    printf '%s\n' 4200 > "$CASE_ROOT/core.pid"
    printf '%s\n' 'sentinel: never modify config' > "$CASE_ROOT/lab/config/lab.json"
    printf '%s\n' 'sentinel: never replace binary' > "$CASE_ROOT/lab/bin/xray"
    printf '%s\n' 'sentinel: never rewind ledger' > "$CASE_ROOT/ledger"
    cat > "$CASE_ROOT/lab/bin/zic-lab" <<'STUB'
#!/bin/sh
set -eu
case $1 in
ready) exit 0;;
write-pid) printf '%s\n' "$3" > "$5";;
*) exit 1;;
esac
STUB
    chmod 0700 "$CASE_ROOT/lab/bin/zic-lab"
    printf '%s\n' '#!/bin/bash' '# previous working workaround' > "$CASE_ROOT/lab/lab.sh"
    cp "$project/packaging/lab/lab.sh" "$CASE_ROOT/source/lab.sh"
    cp "$project/packaging/lab/start.sh" "$CASE_ROOT/source/start.sh"
    for role in panel core agent; do
        unit="zic-lab-$role.service"
        sed '/^ExecStartPost=/d;s/^Type=notify$/Type=simple/' "$project/packaging/lab/$unit" > "$CASE_ROOT/etc/$unit"
        cp "$CASE_ROOT/etc/$unit" "$CASE_ROOT/lab/units/$unit"
        cp "$project/packaging/lab/$unit" "$CASE_ROOT/source/units/$unit"
        : > "$CASE_ROOT/$unit.active"
    done
    sed -e "s|/opt/zic-xray-lab|$CASE_ROOT/lab|g" -e "s|/etc/systemd/system|$CASE_ROOT/etc|g" -e "s|/etc/os-release|$CASE_ROOT/os-release|g" -e "s|/run/lock/zic-xray-lab.lock|$CASE_ROOT/operation.lock|g" -e "s|/run/zic-lab-core|$CASE_ROOT/run|g" -e 's/\[\[ $EUID == 0 && /[[ /' -e 's|\[\[ $# == 0 && -x /usr/bin/systemd-notify \]\]|[[ $# == 0 ]]|' "$project/packaging/lab/update-startup.sh" > "$CASE_ROOT/source/update-startup.sh"
    (cd "$CASE_ROOT/source" && sha256sum start.sh lab.sh units/* update-startup.sh > SHA256SUMS)
    sha256sum "$CASE_ROOT/lab/lab.sh" "$CASE_ROOT/etc/"* "$CASE_ROOT/lab/units/"* > "$CASE_ROOT/original.sha256"
    sha256sum "$CASE_ROOT/lab/config/lab.json" "$CASE_ROOT/lab/bin/"* "$CASE_ROOT/ledger" > "$CASE_ROOT/untouched.sha256"
}
run_update() { ZIC_LAB=1 bash "$CASE_ROOT/source/update-startup.sh" "$@" > "$CASE_ROOT/update.log" 2>&1; }
assert_restored() {
    sha256sum --check --strict "$CASE_ROOT/original.sha256" >/dev/null || fail 'rollback did not restore exact files'
    sha256sum --check --strict "$CASE_ROOT/untouched.sha256" >/dev/null || fail 'binary/config/ledger changed'
    [[ ! -e $CASE_ROOT/lab/start.sh && $(cat "$CASE_ROOT/run/xray.pid") == "$(cat "$CASE_ROOT/core.pid")" ]] || fail 'rollback restored stale PID or left new launcher'
    for role in panel core agent; do [[ -f $CASE_ROOT/zic-lab-$role.service.active ]] || fail 'rollback did not restore active services'; done
}
setup_transaction manual
run_update check
[[ ! -e $CASE_ROOT/lab/start.sh && ! -e $CASE_ROOT/commands.log.changed ]] || fail 'preflight changed files'
run_update apply
[[ -f $CASE_ROOT/lab/start.sh ]] || fail 'apply did not install launcher'
backup=$(find "$CASE_ROOT/lab/results" -maxdepth 1 -type d -name 'startup-fix.backup.*')
run_update rollback "$backup"
assert_restored
printf '%s\n' 'PASS apply + manual rollback: exact working workaround restored; fresh PID; binary/config/ledger unchanged'

setup_transaction failed-activation
if FAIL_NEW_START=1 run_update apply; then fail 'failed activation reported success'; fi
assert_restored
printf '%s\n' 'PASS activation failure: automatic rollback and nonzero exit'

setup_transaction edited-after-apply
run_update apply
backup=$(find "$CASE_ROOT/lab/results" -maxdepth 1 -type d -name 'startup-fix.backup.*')
printf '%s\n' '# subsequent operator edit' >> "$CASE_ROOT/lab/lab.sh"
if run_update rollback "$backup"; then fail 'rollback overwrote a subsequent edit'; fi
grep -q 'subsequent operator edit' "$CASE_ROOT/lab/lab.sh" || fail 'subsequent edit was lost'
for role in panel core agent; do [[ -f $CASE_ROOT/zic-lab-$role.service.active ]] || fail 'refused rollback stopped running services'; done
printf '%s\n' 'PASS rollback refuses later edits'

setup_transaction dropin
if FAIL_DROPIN=1 run_update apply; then fail 'unreviewed drop-in accepted'; fi
[[ ! -e $CASE_ROOT/lab/start.sh ]] || fail 'preflight refusal still changed files'
sha256sum --check --strict "$CASE_ROOT/original.sha256" >/dev/null
printf '%s\n' 'PASS unreviewed drop-in refused before mutation'

for unit in zic-lab-panel.service zic-lab-core.service; do
    grep -qx 'DynamicUser=yes' "$project/packaging/lab/$unit"
    grep -qx 'Type=notify' "$project/packaging/lab/$unit"
    ! grep -q '^ExecStartPost=' "$project/packaging/lab/$unit"
    grep -qx 'ProtectSystem=strict' "$project/packaging/lab/$unit"
    grep -qx 'IPAddressDeny=any' "$project/packaging/lab/$unit"
done
printf '%s\n' 'All startup mock regressions PASS. Real Debian systemd/reboot NOT RUN.'
