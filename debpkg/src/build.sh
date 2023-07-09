#!/bin/sh

set -e

export CGO_ENABLED=0

go build -C ministry/cmd/checkbrigadier -o ../../../bin/checkbrigadier
go build -C ministry/cmd/createbrigade -o ../../../bin/createbrigade
go build -C ministry/cmd/restorebrigadier -o ../../../bin/restorebrigadier
go build -C ministry/cmd/syncstats -o ../../../../bin/syncstats

go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

nfpm package --config "ministry/debpkg/nfpm.yaml" --target "${SHARED_BASE}/pkg" --packager deb

chown "${USER_UID}":"${USER_UID}" "${SHARED_BASE}/pkg/"*.deb
