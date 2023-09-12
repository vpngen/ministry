#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}
REASON="inactive"

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

DAYS=${DAYS:-"1"}
NUMS=${NUMS:-"1000"}

CMD="getwasted inactive -m ${DAYS} -n ${NUMS}"
echo "GET WASTED: ${CMD}"

purge_per_realm () {
        REALM="${1}"
        wasted=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${CMD}")
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        for bid in ${wasted}; do
                echo "delete ${bid}"

                psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${bid}" \
                --set reason="${REASON}" \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        INSERT INTO :"schema_name".deleted_brigadiers (brigade_id, reason) VALUES (:'brigade_id',:'reason') ON CONFLICT DO NOTHING;
        INSERT INTO :"schema_name".brigades_actions (brigade_id, event_name, event_info, event_time) VALUES (:'brigade_id', 'delete_brigade', :'reason', now() AT TIME ZONE 'UTC');
COMMIT;
EOF
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with db: $rc"
                        continue
                fi

                del="delbrigade -uuid ${bid}"
                echo "${del}"

                ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${del}"
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with deletion: $rc"
                        continue
                fi
        done
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
        purge_per_realm "${control_ip}"
done