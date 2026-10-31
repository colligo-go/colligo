#!/bin/bash
set -euo pipefail

if [[ ! "${COLLIGO_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]]; then
    echo "COLLIGO_COMMIT must be the 40-character commit of tag v${PKG_VERSION}; set commit in meta.yaml" >&2
    exit 1
fi

export CGO_ENABLED=0
export GOFLAGS="-trimpath -mod=readonly"
export GOPATH="$(mktemp -d)"
export GOCACHE="$(mktemp -d)"

buildinfo=github.com/colligo-go/colligo/internal/buildinfo
go build \
    -ldflags "-X ${buildinfo}.injectedVersion=v${PKG_VERSION} -X ${buildinfo}.injectedCommit=${COLLIGO_COMMIT}" \
    -o "${PREFIX}/bin/colligo" ./cmd/colligo

test "$("${PREFIX}/bin/colligo" version)" = "colligo v${PKG_VERSION} (${COLLIGO_COMMIT})"

go-licenses save ./cmd/colligo --save_path=thirdparty --ignore github.com/colligo-go/colligo

go clean -modcache
