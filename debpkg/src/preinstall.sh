#!/bin/sh

HEAD_ADMIN_USER="vg_head_admin"
PARTNERS_ADMIN_USER="vg_partners_admin"
HEAD_VPNAPI_USER="vg_head_vpnapi"
HEAD_STATS_USER="vg_head_stats"
HEAD_MIGRATION_USER="vg_head_migr"

create_users () {
        if id "${HEAD_ADMIN_USER}" >/dev/null 2>&1; then
                echo "user ${HEAD_ADMIN_USER} already exists"
        else
                useradd -p "*" -m "${HEAD_ADMIN_USER}" -s /bin/bash
        fi

        if id "${PARTNERS_ADMIN_USER}" >/dev/null 2>&1; then
                echo "user ${PARTNERS_ADMIN_USER} already exists"
        else
                useradd -p "*" -m "${PARTNERS_ADMIN_USER}" -s /bin/bash
        fi

        if id "${HEAD_VPNAPI_USER}" >/dev/null 2>&1; then
                echo "user ${HEAD_VPNAPI_USER} already exists"
        else
                useradd -p "*" -m "${HEAD_VPNAPI_USER}" -s /bin/bash
        fi

        if id "${HEAD_STATS_USER}" >/dev/null 2>&1; then
                echo "user ${HEAD_STATS_USER} already exists"
        else
                useradd -p "*" -m "${HEAD_STATS_USER}" -s /bin/bash
        fi

        if id "${HEAD_MIGRATION_USER}" >/dev/null 2>&1; then
                echo "user ${HEAD_MIGRATION_USER} already exists"
        else
                useradd -p "*" -m "${HEAD_MIGRATION_USER}" -s /bin/bash
        fi
}

cleanInstall() {
	printf "Pre Install of an clean install\n"

        set -e

        # Create new users
        create_users
}

upgrade() {
    	printf "Pre Install of an upgrade\n"

        # Create new users
        create_users

        systemctl stop vg-sync-ids.timer ||:
        systemctl stop vg-sync-ids.service ||:
        systemctl stop vg-ckvip.timer ||:
        systemctl stop vg-ckvip.service ||:
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
    printf "\033[31m install... \033[0m\n"
    cleanInstall
    ;;
  "2" | "upgrade")
    printf "\033[31m upgrade... \033[0m\n"
    upgrade
    ;;
  *)
    # $1 == version being installed
    printf "\033[31m default... \033[0m\n"
    cleanInstall
    ;;
esac


