#!/bin/sh

DBUSER=${DBUSER:-"postgres"}
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA_PAIRS:-"head"}

HEAD_ADMIN_DBUSER=${HEAD_ADMIN_DBUSER:-"vg_head_admin"}
echo "head admin user: $HEAD_ADMIN_DBUSER"
HEAD_VPNAPI_DBUSER=${HEAD_VPNAPI_DBUSER:-"vg_head_vpnapi"}
echo "head vpnapi user: $HEAD_VPNAPI_DBUSER"
PARTNERS_ADMIN_DBUSER=${PARTNERS_ADMIN_DBUSER:-"vg_partners_admin"}
echo "partners admin user: $PARTNERS_ADMIN_DBUSER"
HEAD_STATS_DBUSER=${HEAD_STATS_DBUSER:-"vg_head_stats"}
echo "head stats user: $HEAD_STATS_DBUSER"

SQL_DIR="/usr/share/vg-head/sql"

load_sql_file () {
        cat "$1" | sudo -u "${DBUSER}" psql -d "${DBNAME}" -v ON_ERROR_STOP=yes \
                --set schema_name="${SCHEMA}" \
                --set head_stats_dbuser="${HEAD_STATS_DBUSER}" \
                --set head_admin_dbuser="${HEAD_ADMIN_DBUSER}" \
                --set head_vpnapi_dbuser="${HEAD_VPNAPI_DBUSER}" \
                --set partners_admin_dbuser="${PARTNERS_ADMIN_DBUSER}"
        rc=$?
        if [ ${rc} -ne 0 ] && [ ${rc} -ne 3 ]; then
                exit 1
        fi
}

init_database () {
        # Create database
        echo "CREATE DATABASE :dbname;" | sudo -u "${DBUSER}" psql --set dbname="${DBNAME}" -v ON_ERROR_STOP=yes
        rc=$?
        if [ ${rc} -ne 0 ]; then
                exit 1
        fi

        # Init database

        load_sql_file "${SQL_DIR}/init/000-versioning.sql"
        load_sql_file "${SQL_DIR}/init/001-init.sql"
        load_sql_file "${SQL_DIR}/init/002-roles.sql"

        rm -f "${SQL_DIR}/init/*.sql"
}

apply_database_patches () {
        for patch in "${SQL_DIR}/patches/"*.sql; do
                load_sql_file "${patch}"
        done

        sudo -u "${DBUSER}" psql -v ON_ERROR_STOP=yes -c "SELECT pg_reload_conf();"
        rc=$?
        if [ ${rc} -ne 0 ]; then
                exit 1
        fi

        rm -f "${SQL_DIR}/patches/*.sql"
}


cleanInstall() {
	printf "Post Install of an clean install\n"

        set -e

        init_database
        apply_database_patches

    	printf "Reload the service unit from disk\n"
    	systemctl daemon-reload ||:
}

upgrade() {
    	printf "Post Install of an upgrade\n"

        apply_database_patches

    	printf "Reload the service unit from disk\n"
    	systemctl daemon-reload ||:
}

# Step 2, check if this is a clean install or an upgrade
action="$1"
if  [ "$1" = "configure" ] && [ -z "$2" ]; then
 	# Alpine linux does not pass args, and deb passes $1=configure
 	action="install"
elif [ "$1" = "configure" ] && [ -n "$2" ]; then
   	# deb passes $1=configure $2=<current version>
	action="upgrade"
fi

case "$action" in
  "1" | "install")
    cleanInstall
    ;;
  "2" | "upgrade")
    printf "\033[32m Post Install of an upgrade\033[0m\n"
    upgrade
    ;;
  *)
    # $1 == version being installed
    printf "\033[32m Alpine\033[0m"
    cleanInstall
    ;;
esac


