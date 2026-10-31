#!/bin/sh
# Run only as ExecStart: the readiness child inherits this credential mount
# namespace. ExecStartPost on systemd 252 does not get that same view.
set -eu
umask 077
lab_root=/opt/zic-xray-lab
: "${CREDENTIALS_DIRECTORY:?systemd credentials required}"
: "${NOTIFY_SOCKET:?systemd notify socket required}"
[ "$#" -eq 1 ] || exit 1
main_pid=$$
case $1 in
panel)
    (
        "$lab_root/bin/zic-lab" ready --config "$CREDENTIALS_DIRECTORY/lab.json" --panel-only --ca "$CREDENTIALS_DIRECTORY/ca.pem"
        # Keep the default notification barrier: do not use --no-block.
        exec /usr/bin/systemd-notify --ready --pid="$main_pid"
    ) &
    exec "$lab_root/bin/zic-lab" serve --config "$CREDENTIALS_DIRECTORY/lab.json" --cert "$CREDENTIALS_DIRECTORY/cert.pem" --key "$CREDENTIALS_DIRECTORY/key.pem"
    ;;
core)
    # exec keeps this PID when the shell becomes the OFFICIAL core binary.
    # Run on EVERY start, including Restart=on-failure and boot. /run is volatile.
    "$lab_root/bin/zic-lab" write-pid --pid "$main_pid" --out /run/zic-lab-core/xray.pid
    (
        "$lab_root/bin/zic-lab" ready --config "$CREDENTIALS_DIRECTORY/lab.json" --core-only
        exec /usr/bin/systemd-notify --ready --pid="$main_pid"
    ) &
    exec "$lab_root/bin/xray" run -config "$CREDENTIALS_DIRECTORY/xray.json"
    ;;
*) exit 1;;
esac
