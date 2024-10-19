#!/bin/sh

# interpret first argument as command
# pass rest args to scripts

printdef() {
    echo "Usage: <command> <args...>"
    exit 1
}

if [ $# -eq 0 ]; then 
    printdef
fi

cmd=${1}; shift
basedir=$(dirname "$0")

if [ "createbrigade" = "${cmd}" ]; then
    "${basedir}"/createbrigade "$@"
elif [ "restorebrigadier" = "${cmd}" ]; then
    "${basedir}"/restorebrigadier "$@"
elif [ "synclabels" = "${cmd}" ]; then
    "${basedir}"/synclabels "$@"
else
    echo "Unknown command: ${cmd}"
    printdef
fi
