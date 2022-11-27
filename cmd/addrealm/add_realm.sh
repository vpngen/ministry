#!/bin/sh

set -e

CONFDIR=${CONFDIR:-"/etc/vgdept"}
echo "confdir: ${CONFDIR}"
DBNAME=${DBNAME:-$(cat ${CONFDIR}/dbname)}
echo "dbname: $DBNAME"
SCHEMA=${SCHEMA:-$(cat ${CONFDIR}/schema)}
echo "schema: $SCHEMA"

realm_id="$1"
control_ip="$2"
shift; shift


if [ "x" = "x${realm_id}" -o "x" = "x${control_ip}" ]; then
    echo "Usage: $0 <realm_id> <control_ip>"
    exit 1
fi

ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set realm_id=${realm_id} \
    --set control_ip=${control_ip} <<EOF
BEGIN;
INSERT INTO :"schema_name".realms (realm_id,control_ip,is_active) VALUES (:'realm_id', :'control_ip', false);
COMMIT;
EOF
