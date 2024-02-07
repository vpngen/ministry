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
        JOIN :"schema_name".brigadier_realms br ON b.brigade_id=br.brigade_id
        JOIN :"schema_name".realms r ON br.realm_id=r.realm_id
        LEFT JOIN :"schema_name".deleted_brigadiers d ON b.brigade_id=d.brigade_id
WHERE
        br.featured = true
        AND d.brigade_id IS NULL
        AND (
                SELECT
                        COUNT(*)
                FROM
                        :"schema_name".brigadier_realms
                WHERE
                        brigade_id = b.brigade_id
                        AND draft = false
                        AND featured = false
        ) = 0
EOF
)

for line in ${list} ; do
        bid=$(echo "${line}" | cut -f 1 -d "|")
        realm=$(echo "${line}" | cut -f 2 -d "|")

        check="checkbrigade -uuid ${bid}"

        # TODO: check strict error
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
FROM
        :"schema_name".brigadiers AS b
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

        "$(dirname "$0")"/delete_brigadier.sh "${bid}" "${REASON}"   
done