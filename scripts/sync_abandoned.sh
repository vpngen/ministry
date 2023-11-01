#!/bin/sh

ETCDIR="${HOME}/.ssh"
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}
USERNAME="_valera_"
REASON=${REASON:-"abandoned_deletion"}

list=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t -A --set schema_name="${SCHEMA}" <<EOF
SELECT
        b.brigade_id,
        r.control_ip
FROM
        :"schema_name".brigadiers b
        JOIN :"schema_name".realms r ON b.realm_id=r.realm_id
        LEFT JOIN :"schema_name".deleted_brigadiers d ON b.brigade_id=d.brigade_id
WHERE
        d.deleted_at IS NULL
EOF
)

for line in ${list} ; do
        bid=$(echo "${line}" | cut -f 1 -d "|")
        realm=$(echo "${line}" | cut -f 2 -d "|")

        check="checkbrigade -uuid ${bid}"

        out=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${ETCDIR}"/id_ed25519 -o StrictHostKeyChecking=no "${USERNAME}@${realm}" "${check}")
        rc=$?
        if [ $rc -eq 0 ]; then
                continue
        fi

        echo "[+]         Difinitly user is not exists: $bid: $rc: $out"

        info=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t --set schema_name="${SCHEMA}" --set bid="${bid}" <<EOF
SELECT
        b.brigade_id,
        b.brigadier,
        b.created_at,
        b.realm_id,
        p.partner
FROM
        :"schema_name".brigadiers AS b
        JOIN :"schema_name".partners p ON b.partner_id=p.partner_id
WHERE
        b.brigade_id=:'bid'
EOF
)

        echo "Info: ${info}"

        actions=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t --set schema_name="${SCHEMA}" --set bid="${bid}" <<EOF
SELECT
        *
FROM
        :"schema_name".brigades_actions
WHERE
        brigade_id=:'bid'
EOF
)

        echo "Actions: ${actions}"

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
done