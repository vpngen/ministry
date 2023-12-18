#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}

if [ -z "${SSH_KEY}" ]; then
        if [ -s "${HOME}/.ssh/id_ed25519" ]; then
                SSH_KEY="${HOME}/.ssh/id_ed25519"
        elif [ -s "${HOME}/.ssh/id_ecdsa" ]; then
                SSH_KEY="${HOME}/.ssh/id_ecdsa"
        elif [ -s "/etc/vgdept/id_ed25519" ]; then
                SSH_KEY="/etc/vgdept/id_ed25519"
        elif [ -s "/etc/vgdept/id_ecdsa" ]; then
                SSH_KEY="/etc/vgdept/id_ecdsa"
        else
                echo "[-]         SSH key not found"
                exit 1
        fi
fi

if [ $# -eq 2 ]; then
        REALM_ID="${1}"
        CONTROL_IP="${2}"

        if [ -z "${REALM_ID}" ] || [ -z "${CONTROL_IP}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        CMD="get_free_slots -fa"

        num=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no -T "${USERNAME}"@"${CONTROL_IP}" "${CMD}")
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        echo "[+]         Free slots: ${num}"

        if [ -z "${num}" ]; then
                echo "[-]         Free slots is empty"
                exit 1
        fi

        psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set realm_id="${REALM_ID}" \
                --set free_slots="${num}" \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        UPDATE :"schema_name".realms SET free_slots = :'free_slots' WHERE realm_id = :'realm_id';
COMMIT;
EOF
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with db: $rc"
                exit 1
        fi

        exit 0
fi

realms=$(psql -d "${DBNAME}" \
        -q -t -A --csv \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" << EOF
        SELECT realm_id,control_ip FROM :"schema_name".realms WHERE is_active = true;
EOF
)

for realm in ${realms}; do
        realm_id=$(echo "${realm}" | cut -d ',' -f 1)
        control_ip=$(echo "${realm}" | cut -d ',' -f 2)

        echo "[+]     Realm: ${realm_id} control_ip: ${control_ip}"

        spinlock_filename="${realm_id}.lock"
        if [ -d "${HOME}" ] && [ -w "${HOME}" ]; then
                spinlock="${HOME}/${spinlock_filename}"
        elif [ -d "/tmp" ] && [ -w "/tmp" ]; then
                spinlock="/tmp/${spinlock_filename}"
        else
                echo "[-]         Can't create spinlock file"
                exit 1
        fi

        flock -x -w "${LOCK_TIMEOUT}" "${spinlock}" "${0}" "${control_ip}" "${realm_id}"
done
