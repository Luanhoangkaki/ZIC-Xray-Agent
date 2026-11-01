#!/bin/bash
# Synthetic archive only: never runs an ELF, installer or service.
set -euo pipefail
umask 077
project=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'printf "Archive mock evidence: %s\n" "$work"' EXIT
for arch in amd64 arm64; do
    name="zic-xray-agent-installer-debian12-$arch-v1.0.1"
    mkdir -p "$work/$name/bin/linux-$arch" "$work/$name/validation/linux-$arch"
    for relative in "bin/linux-$arch/zic-xray-agent" "validation/linux-$arch/xray" install.sh rollback-install.sh README.md; do
        printf '%s\n' 'synthetic, never execute' > "$work/$name/$relative"
        chmod 0600 "$work/$name/$relative"
    done
    bash "$project/scripts/archive-installer.sh" "$work" "$name" "$arch" "$work/$arch.tar.gz"
    LC_ALL=C tar -tvzf "$work/$arch.tar.gz" > "$work/$arch.modes"
    for relative in "bin/linux-$arch/zic-xray-agent" "validation/linux-$arch/xray" install.sh rollback-install.sh; do
        awk -v path="$name/$relative" '$NF==path {if($1!="-rwx------")exit 1;found++} END {if(found!=1)exit 1}' "$work/$arch.modes"
    done
    awk -v path="$name/README.md" '$NF==path {if($1!="-rw-------")exit 1;found++} END {if(found!=1)exit 1}' "$work/$arch.modes"
    if bash "$project/scripts/archive-installer.sh" "$work" "$name" "$arch" "$work/$arch.tar.gz"; then exit 1; fi
done
printf '%s\n' 'Archive regressions PASS both targets: explicit 0700 executable / 0600 data headers; existing archive refused. Linux execution NOT RUN.'
