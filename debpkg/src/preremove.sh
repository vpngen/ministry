#!/bin/sh

remove() {
        printf "Pre Remove of a normal remove\n"

        printf "Stop the service unit\n"

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
  "failed-upgrade")
    upgrade
    ;;
  *)
    printf "\033[32m Alpine\033[0m"
    remove
    ;;
esac
