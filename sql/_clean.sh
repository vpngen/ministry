#!/bin/sh

set -e

DBNAME=${DBNAME:-"vgdept"}
echo "dbname: $DBNAME"
SCHEMA=${SCHEMA:-""}
echo "schema: $SCHEMA"
HEAD_ADMIN_DBUSER=${HEAD_ADMIN_DBUSER:-"vg_head_admin"}
echo "head admin user: $HEAD_ADMIN_DBUSER"
HEAD_VPNAPI_DBUSER=${HEAD_VPNAPI_DBUSER:-"vg_head_vpnapi"}
echo "head vpnapi user: $HEAD_VPNAPI_DBUSER"
PARTNERS_ADMIN_DBUSER=${PARTNERS_ADMIN_DBUSER:-"vg_partners_admin"}
echo "partners admin user: $PARTNERS_ADMIN_DBUSER"
HEAD_STATS_DBUSER=${HEAD_STATS_DBUSER:-"vg_head_stats"}
echo "head stats user: $HEAD_STATS_DBUSER"

cat <<EOF | sudo -i -u postgres psql  \
    --set schema_name="${SCHEMA}" \
    --set head_stats_dbuser="${HEAD_STATS_DBUSER}" \
    --set head_admin_dbuser="${HEAD_ADMIN_DBUSER}" \
    --set head_vpnapi_dbuser="${HEAD_VPNAPI_DBUSER}" \
    --set partners_admin_dbuser="${PARTNERS_ADMIN_DBUSER}"
DROP DATABASE ${DBNAME};
DROP ROLE ${HEAD_STATS_DBUSER};
DROP ROLE ${HEAD_ADMIN_DBUSER};
DROP ROLE ${HEAD_VPNAPI_DBUSER};
DROP ROLE ${PARTNERS_ADMIN_DBUSER};
CREATE DATABASE ${DBNAME};
EOF
