#!/bin/bash
# Linux acceptance runner guard logic only. No native core/service is executed.
set -euo pipefail
umask 077
project=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/zic-runner-guard.XXXXXXXX")
mkdir -p "$work/bin"
printf 'ID=debian\nVERSION_ID=12\n' > "$work/os-release"
sed "s|/etc/os-release|$work/os-release|g" "$project/scripts/test-debian12.sh" > "$work/runner.sh"
cat > "$work/bin/uname" <<'STUB'
#!/bin/sh
case $1 in -s) echo Linux;;-m) echo x86_64;;*) exit 1;;esac
STUB
cat > "$work/bin/systemctl" <<'STUB'
#!/bin/sh
[ "$1" = is-active ] && [ "$3" = "$MOCK_ACTIVE" ]
STUB
cat > "$work/bin/sha256sum" <<'STUB'
#!/bin/sh
if [ "${MOCK_BAD_CORE:-0}" = 1 ]; then printf '%064d  core\n' 0; else printf '%s  core\n' e783b9d2b46ed1ab3617c59b04dfbce38c999322d018c6e060f18d88bf6158aa; fi
STUB
cat > "$work/core" <<'STUB'
#!/bin/sh
: > "$CORE_EXECUTION_MARKER"
exit 99
STUB
chmod 0700 "$work/core" "$work/bin/"*
export CORE_EXECUTION_MARKER="$work/executed" MOCK_ACTIVE=none
if PATH="$work/bin:$PATH" MOCK_BAD_CORE=1 ZIC_LAB=1 XRAY_BINARY="$work/core" bash "$work/runner.sh" > "$work/bad-core.log" 2>&1; then exit 1; fi
grep -q 'checksum mismatch' "$work/bad-core.log"
[[ ! -e $work/executed ]]
for MOCK_ACTIVE in xray.service zic-xray-agent.service zic-lab-panel.service zic-lab-core.service zic-lab-agent.service; do
    export MOCK_ACTIVE
    if PATH="$work/bin:$PATH" ZIC_LAB=1 XRAY_BINARY="$work/core" bash "$work/runner.sh" > "$work/active.log" 2>&1; then exit 1; fi
    grep -q 'Refusing: an existing Xray/agent/Lab service is active' "$work/active.log"
    [[ ! -e $work/executed ]]
done
printf '%s\n' 'PASS Debian runner mocks: bad core checksum and all five active-service guards refuse BEFORE core execution; no service calls beyond mocked reads'
