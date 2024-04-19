#!/bin/sh

HEAD_ADMIN_USER="vg_head_admin"
PARTNERS_ADMIN_USER="vg_partners_admin"
HEAD_VPNAPI_USER="vg_head_vpnapi"
HEAD_STATS_USER="vg_head_stats"
HEAD_MIGRATION_USER="vg_head_migr"

remove_users () {
        if id "${HEAD_ADMIN_USER}" >/dev/null 2>&1; then
                userdel -r "${HEAD_ADMIN_USER}"
        else
                echo "user ${HEAD_ADMIN_USER} does not exists"
        fi

        if id "${PARTNERS_ADMIN_USER}" >/dev/null 2>&1; then
                echo "user ${PARTNERS_ADMIN_USER} already exists"
        else
                useradd -p "*" -m "${PARTNERS_ADMIN_USER}" -s /bin/bash
        fi
        
        if id "${HEAD_VPNAPI_USER}" >/dev/null 2>&1; then
                userdel -r "${HEAD_VPNAPI_USER}"
        else
                echo "user ${HEAD_VPNAPI_USER} does not exists"
        fi

        if id "${HEAD_STATS_USER}" >/dev/null 2>&1; then
                userdel -r "${HEAD_STATS_USER}"
        else
                echo "user ${HEAD_STATS_USER} does not exists"
        fi

        if id "${HEAD_MIGRATION_USER}" >/dev/null 2>&1; then
                userdel -r "${HEAD_MIGRATION_USER}"
        else
                echo "user ${HEAD_MIGRATION_USER} does not exists"
        fi
}

remove() {
        printf "Post Remove of a normal remove\n"

        remove_users

        printf "Reload the service unit from disk\n"
        systemctl daemon-reload ||:
}

purge() {
    printf "\033[32m Pre Remove purge, deb only\033[0m\n"
}

upgrade() {
    printf "\033[32m Pre Remove of an upgrade\033[0m\n"
}

echo "$@"

action="$1"

case "$action" in
  "0" | "remove")
    remove
    ;;
  "1" | "upgrade")
    upgrade
    ;;
  "purge")
    purge
    ;;
  *)
    printf "\033[32m Alpine\033[0m"
    remove
    ;;
esac
