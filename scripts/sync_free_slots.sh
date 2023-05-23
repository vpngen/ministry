#!/bin/sh

SSH_KEY=${SSH_KEY:-"/etc/vgdept/id_ed25519"}
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"library"}
USERNAME=${USERNAME:-"_valera_"}


get_realms_free_slot () {
        REALM_ID="${1}"
        CONTROL_IP="${2}"

        if [ -z "${REALM_ID}" ] || [ -z "${CONTROL_IP}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        CMD="get_free_slots -a"

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
}

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
        get_realms_free_slot "${realm_id}" "${control_ip}"
done
