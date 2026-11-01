#!/bin/bash
set -euo pipefail
[[ $# -eq 0 || ( $# -eq 1 && $1 == --include-tests ) ]] || { printf '%s\n' 'Usage: bash scripts/build.sh [--include-tests]' >&2; exit 1; }
include_tests=${1:-}
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export CGO_ENABLED=0
manifest=$(mktemp)
trap 'rm -f -- "$manifest"' EXIT
for arch in amd64 arm64; do
    mkdir -p -- "dist/linux-$arch"
    for component in zic-xray-agent zic-bench zic-lab; do
        GOOS=linux GOARCH="$arch" go build -buildvcs=false -trimpath -ldflags '-s -w' -o "dist/linux-$arch/$component" "./cmd/$component"
        (cd dist && sha256sum "linux-$arch/$component") >> "$manifest"
    done
    if [[ $include_tests == --include-tests ]]; then
        mkdir -p -- "dist/lab-tests/linux-$arch"
        packages=$(GOOS=linux GOARCH="$arch" go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
        while IFS= read -r package; do
            [[ -n $package ]] || continue
            name=${package#zic-xray-agent/}
            name=${name//\//-}.test
            GOOS=linux GOARCH="$arch" go test -c -buildvcs=false -trimpath -ldflags '-s -w' -o "dist/lab-tests/linux-$arch/$name" "$package"
            (cd dist && sha256sum "lab-tests/linux-$arch/$name") >> "$manifest"
        done <<< "$packages"
    fi
done
mv -- "$manifest" dist/SHA256SUMS
