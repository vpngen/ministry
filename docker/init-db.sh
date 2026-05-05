#!/bin/bash
set -e

SCHEMA="${SCHEMA:-head}"
HEAD_ADMIN_DBUSER="${HEAD_ADMIN_DBUSER:-vg_head_admin}"
HEAD_VPNAPI_DBUSER="${HEAD_VPNAPI_DBUSER:-vg_head_vpnapi}"
PARTNERS_ADMIN_DBUSER="${PARTNERS_ADMIN_DBUSER:-vg_partners_admin}"
HEAD_STATS_DBUSER="${HEAD_STATS_DBUSER:-vg_head_stats}"
HEAD_MIGRATION_DBUSER="${HEAD_MIGRATION_DBUSER:-vg_head_migr}"

run_sql() {
    psql -v ON_ERROR_STOP=1 \
        --username "$POSTGRES_USER" \
        --dbname "$POSTGRES_DB" \
        --set schema_name="$SCHEMA" \
        --set head_admin_dbuser="$HEAD_ADMIN_DBUSER" \
        --set head_vpnapi_dbuser="$HEAD_VPNAPI_DBUSER" \
        --set partners_admin_dbuser="$PARTNERS_ADMIN_DBUSER" \
        --set head_stats_dbuser="$HEAD_STATS_DBUSER" \
        --set head_migration_dbuser="$HEAD_MIGRATION_DBUSER"
}

cat /sql/init/*.sql | run_sql
cat /sql/patch/*.sql | run_sql
