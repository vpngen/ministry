#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}

LOCK_TIMEOUT=${LOCK_TIMEOUT:-"120"} # seconds

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

if [ "$#" -eq 2 ]; then
        control_ip="${1}"
        brigade_id="${2}"

        echo "[+] Try to delete: Brigade: ${brigade_id} control_ip: ${control_ip}"

        del="delbrigade -uuid ${brigade_id}"
        echo "${del}"

        out=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${control_ip}" "${del}" 2>&1)
        rc=$?
        if [ $rc -ne 0 ]; then
                if ! echo "${out}" | grep -q "Can't get control ip" | grep -q "no rows in result set"; then
                        echo "[-]         Something wrong with deletion: $rc"

                        exit 1
                fi

                echo "[+]         Brigade not found"

                psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${brigade_id}" \
                --set realm_id="${realm_id}" \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        DELETE FROM 
                :"schema_name".brigadier_realms 
        WHERE 
                brigadier_realms.brigade_id=:'brigade_id' 
        AND 
                brigadier_realms.realm_id=:'realm_id'
        AND
                brigadier_realms.draft = true;

        INSERT INTO 
                :"schema_name".brigadier_realms_actions 
                (brigade_id, realm_id, event_name, event_info, event_time)
        VALUES 
                (:'brigade_id', :'realm_id', 'remove', 'd', now() AT TIME ZONE 'UTC');
COMMIT;
EOF
        fi

        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with db: $rc"
                exit 1
        fi

        exit 0
fi

brigadier_realms=$(psql -d "${DBNAME}" \
        -q -t -A --csv \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" << EOF
        SELECT 
                br.brigade_id, br.realm_id, r.control_ip
        FROM 
                :"schema_name".brigadier_realms br
                JOIN :"schema_name".realms r ON br.realm_id = r.realm_id
        WHERE 
                r.is_active = true
                AND br.draft = true
                AND br.update_time < NOW() AT TIME ZONE 'UTC' - interval '10 minutes';
EOF
)

for realm in ${brigadier_realms}; do
        brigade_id=$(echo "${realm}" | cut -d ',' -f 1)
        realm_id=$(echo "${realm}" | cut -d ',' -f 2)
        control_ip=$(echo "${realm}" | cut -d ',' -f 3)

        echo "[+]   Brigade: ${brigade_id} Realm: ${realm_id} control_ip: ${control_ip}"

        spinlock_filename="${realm_id}.lock"
        if [ -d "${HOME}" ] && [ -w "${HOME}" ]; then
                spinlock="${HOME}/${spinlock_filename}"
        elif [ -d "/tmp" ] && [ -w "/tmp" ]; then
                spinlock="/tmp/${spinlock_filename}"
        else
                echo "[-]         Can't create spinlock file"
                exit 1
        fi

        flock -x -w "${LOCK_TIMEOUT}" "${spinlock}" "${0}" "${control_ip}" "${brigade_id}"
done
