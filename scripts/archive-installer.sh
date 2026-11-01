#!/bin/bash
# Package Unix permissions explicitly; Windows ZIP extraction loses mode bits.
set -euo pipefail
umask 077
[[ $# == 4 ]] || exit 1
parent=$1; name=$2; arch=$3; archive=$4
[[ $name =~ ^zic-xray-agent-installer-debian12-(amd64|arm64)-[a-zA-Z0-9.-]+$ && ( $arch == amd64 || $arch == arm64 ) && ! -e $archive ]] || exit 1
work=$(mktemp -d)
trap 'rm -f -- "$work/exec" "$work/data" "$work/bundle.tar"; rmdir -- "$work"' EXIT
cd -- "$parent"
[[ -d $name && ! -L $name ]]
[[ -z $(find "$name" ! -type f ! -type d -print -quit) ]] || exit 1
agent="$name/bin/linux-$arch/zic-xray-agent"
core="$name/validation/linux-$arch/xray"
[[ -f $agent && -f $core ]]
find "$name" -type d -print0 > "$work/exec"
printf '%s\0' "$agent" "$core" "$name/install.sh" "$name/rollback-install.sh" >> "$work/exec"
find "$name" -type f ! -path "$agent" ! -path "$core" ! -path "$name/install.sh" ! -path "$name/rollback-install.sh" -print0 > "$work/data"
# --no-recursion avoids picking up the same member with Windows-derived modes.
tar --create --format=ustar --file "$work/bundle.tar" --owner=0 --group=0 --numeric-owner --mode=0700 --no-recursion --null --files-from "$work/exec"
tar --append --file "$work/bundle.tar" --owner=0 --group=0 --numeric-owner --mode=0600 --no-recursion --null --files-from "$work/data"
gzip -n -c -- "$work/bundle.tar" > "$archive"
