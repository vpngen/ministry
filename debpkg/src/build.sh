#!/bin/sh

set -e

export CGO_ENABLED=0

go build -C ministry/cmd/checkbrigadier -o ../../../bin/checkbrigadier
go build -C ministry/cmd/ckvip -o ../../../bin/ckvip
go build -C ministry/cmd/reqvipid -o ../../../bin/reqvipid
go build -C ministry/cmd/createbrigade -o ../../../bin/createbrigade
go build -C ministry/cmd/restorebrigadier -o ../../../bin/restorebrigadier
go build -C ministry/cmd/syncstats -o ../../../bin/syncstats
go build -C ministry/cmd/recodesnaps -o ../../../bin/recodesnaps
go build -C ministry/cmd/recodesnapmap -o ../../../bin/recodesnapmap
go build -C ministry/cmd/synclabels -o ../../../bin/synclabels

go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.1 # fix go 1.24

nfpm package --config "ministry/debpkg/nfpm.yaml" --target "${SHARED_BASE}/pkg" --packager deb

chown "${USER_UID}":"${USER_UID}" "${SHARED_BASE}/pkg/"*.deb
