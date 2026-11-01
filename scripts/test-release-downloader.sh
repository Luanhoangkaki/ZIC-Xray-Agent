#!/bin/bash
# Tests ONLY local release fixtures and command mocks. No network or installer.
set -euo pipefail
umask 077
host_os=$(uname -s)
[[ $# == 2 && $2 =~ ^v1\.0\.1-[a-z0-9-]{1,24}$ ]] || { printf '%s\n' 'Usage: bash scripts/test-release-downloader.sh RELEASE_DIRECTORY EXPECTED_TAG' >&2; exit 1; }
export EXPECTED_RELEASE_TAG=$2
release=$(CDPATH= cd -- "$1" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/zic-release-download-test.XXXXXXXX")
trap 'printf "Downloader mock evidence: %s\n" "$work"' EXIT
mkdir -p "$work/mock-bin"
cat > "$work/os-release" <<'STUB'
ID=debian
VERSION_ID=12
STUB
cat > "$work/mock-bin/uname" <<'STUB'
#!/bin/sh
case $1 in -s) printf '%s\n' Linux;;-m) printf '%s\n' "${MOCK_ARCH:-x86_64}";;*) exit 1;;esac
STUB
cat > "$work/mock-bin/systemctl" <<'STUB'
#!/bin/sh
[ "${MOCK_LAB_ACTIVE:-0}" = 1 ]
STUB
cat > "$work/mock-bin/curl" <<'STUB'
#!/bin/bash
set -euo pipefail
out=;url=${@: -1}
while [[ $# -gt 0 ]]; do if [[ $1 == --output ]]; then out=$2;shift;fi;shift;done
printf '%s\n' "$url" >> "$CASE_ROOT/urls.log"
[[ $url == "https://github.com/Luanhoangkaki/ZIC-Xray-Agent/releases/download/$EXPECTED_RELEASE_TAG/"* ]] || exit 1
asset=${url##*/}
if [[ $asset == *.sha256 && ${MOCK_BAD_SIDECAR:-0} == 1 ]]; then printf '%064d  wrong.tar.gz\n' 0 > "$out";exit 0;fi
if [[ $asset == *.tar.gz && ${MOCK_BAD_ARCHIVE:-0} == 1 ]]; then printf '%s\n' corrupt > "$out";exit 0;fi
cp -- "$FIXTURE_RELEASE/$asset" "$out"
STUB
cat > "$work/mock-bin/tar" <<'STUB'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$CASE_ROOT/tar.log"
exec /usr/bin/tar "$@"
STUB
cat > "$work/mock-bin/bash" <<'STUB'
#!/bin/bash
set -euo pipefail
if [[ ${1:-} == */install.sh ]]; then
    [[ $# == 1 && -f $1 && -f ${1%/*}/SHA256SUMS ]] || exit 1
    (cd "${1%/*}" && sha256sum --check --strict SHA256SUMS >/dev/null)
    printf '%s\n' 'verified installer invocation' > "$CASE_ROOT/install-invoked"
    exit 0
fi
exec /bin/bash "$@"
STUB
cat > "$work/mock-bin/sudo" <<'STUB'
#!/bin/sh
exec "$@"
STUB
cat > "$work/mock-bin/chmod" <<'STUB'
#!/bin/sh
[ "${MOCK_MODE_FAIL:-0}" = 0 ] || exit 1
exec /usr/bin/chmod "$@"
STUB
chmod 0700 "$work/mock-bin/"*
export PATH="$work/mock-bin:$PATH" FIXTURE_RELEASE="$release"
# Release URL/version/pins stay intact. NTFS/MSYS cannot represent ELF execute
# permission; only its test copy replaces -x with -f, plus real tar mode checks.
sed "s|/etc/os-release|$work/os-release|g" "$release/get-zic-xray-agent-v1.0.1.sh" > "$work/downloader.sh"
if [[ $host_os == MINGW* || $host_os == MSYS* ]]; then
    grep -Fq '[[ -x $work/$name/bin/linux-$arch/zic-xray-agent && -x $work/$name/validation/linux-$arch/xray ]]' "$work/downloader.sh"
    sed -i 's/\[\[ -x /[[ -f /;s/ && -x / \&\& -f /' "$work/downloader.sh"
    printf '%s\n' 'MSYS executable checks mocked with file existence + archive headers; Linux permissions NOT RUN.'
fi
run_case() {
    export CASE_ROOT="$work/$1";shift
    mkdir -p "$CASE_ROOT/tmp"
    TMPDIR="$CASE_ROOT/tmp" GITHUB_REPOSITORY=attacker/ignored ZIC_RELEASE_VERSION=latest bash "$work/downloader.sh" "$@" > "$CASE_ROOT/output.log" 2>&1
}
for target in x86_64 aarch64; do
    MOCK_ARCH="$target" run_case "valid-$target"
    [[ -f $CASE_ROOT/tar.log && $(find "$CASE_ROOT/tmp" -name install.sh | wc -l) -eq 1 ]] || { printf '%s\n' 'FAIL valid extraction';exit 1; }
    case $target in x86_64) arch=amd64;;aarch64) arch=arm64;;esac
    core=$(find "$CASE_ROOT/tmp" -path "*/validation/linux-$arch/xray")
    [[ -n $core && -f $core ]] || exit 1
    if [[ $host_os != MINGW* && $host_os != MSYS* ]]; then [[ -x $core ]] || exit 1; fi
    LC_ALL=C tar -tvzf "$release/zic-xray-agent-installer-debian12-$arch-v1.0.1.tar.gz" | awk -v tail="/validation/linux-$arch/xray" 'index($NF,tail)==length($NF)-length(tail)+1 {if($1!="-rwx------")exit 1;found++} END {if(found!=1)exit 1}'
    ! grep -q 'attacker\|/latest/' "$CASE_ROOT/urls.log"
    [[ ! -e $CASE_ROOT/install-invoked ]] || exit 1
    MOCK_ARCH="$target" run_case "install-$target" --install
    [[ -f $CASE_ROOT/install-invoked ]] || exit 1
done
printf '%s\n' 'PASS both targets, exact test tag, embedded pins, extract-only default and verified installer dispatch (mock only)'
if MOCK_MODE_FAIL=1 run_case mode-failure --install; then exit 1; fi
[[ ! -e $CASE_ROOT/install-invoked ]] || exit 1
printf '%s\n' 'PASS executable mode failure refuses installer dispatch'
if MOCK_BAD_SIDECAR=1 run_case bad-sidecar;then exit 1;fi
[[ ! -e $CASE_ROOT/tar.log ]] || exit 1
if MOCK_BAD_ARCHIVE=1 run_case bad-archive;then exit 1;fi
[[ ! -e $CASE_ROOT/tar.log ]] || exit 1
printf '%s\n' 'PASS bad sidecar/archive rejected BEFORE any tar operation'
if MOCK_LAB_ACTIVE=1 run_case active-lab --install;then exit 1;fi
[[ ! -e $CASE_ROOT/urls.log && ! -e $CASE_ROOT/tar.log ]] || exit 1
printf '%s\n' 'PASS --install refuses active lab before download/execution'
if MOCK_ARCH=riscv64 run_case wrong-arch;then exit 1;fi
[[ ! -e $CASE_ROOT/urls.log ]] || exit 1
printf '%s\n' 'PASS unsupported architecture; no installer was executed'
