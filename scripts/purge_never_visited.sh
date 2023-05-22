#!/bin/sh

SSHKEY=${SSHKEY:-"/etc/vgdept/id_ed25519"}
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"library"}
USERNAME=${USERNAME:-"_valera_"}
REASON="never_visited"

DAYS=${DAYS:-"1"}
NUMS=${NUMS:-"100"}

CMD="getwasted notvisited -d ${DAYS} -n ${NUMS}"
echo "GET WASTED: ${CMD}"

purge_per_realm () {
        REALM="${1}"
        if [ -z "${REALM}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        wasted=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSHKEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${CMD}")
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        for bid in ${wasted}; do
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

                ssh -o IdentitiesOnly=yes -o IdentityFile="${SSHKEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${del}"
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with deletion: $rc"
                        continue
                fi
        done

        # !!! fetch free slots
}

realms=$(psql -d "${DBNAME}" -q -v ON_ERROR_STOP=yes -t -A -c << EOF
        SELECT realm_id FROM :"schema_name".brigadiers WHERE is_active = true;
EOF
)

for realm in ${realms}; do
        echo "[+]     Realm: ${realm}"
        purge_per_realm "${realm}"
done
