#!/bin/bash
set -euo pipefail

export CGO_ENABLED=0
export GOFLAGS="-trimpath -buildvcs=true -mod=readonly"
export GOPATH="$(mktemp -d)"
export GOCACHE="$(mktemp -d)"

printf '%s\n' conda_build.sh build_env_setup.sh metadata_conda_debug.yaml thirdparty >> .git/info/exclude

go build -o "${PREFIX}/bin/colligo" ./cmd/colligo

"${PREFIX}/bin/colligo" version | grep -q "^colligo v${PKG_VERSION} "

go-licenses save ./cmd/colligo --save_path=thirdparty --ignore github.com/colligo-go/colligo

go clean -modcache
