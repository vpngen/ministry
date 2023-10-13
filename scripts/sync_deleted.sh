#!/bin/sh

ETCDIR="${HOME}/.ssh"
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}
USERNAME="_valera_"


list=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t -A --set schema_name="${SCHEMA}" <<EOF
SELECT
        brigade_id
FROM
        :"schema_name".deleted_brigadiers
EOF
)


for bid in ${list} ; do
        check="checkbrigade -uuid ${bid}"

        ssh -o IdentitiesOnly=yes -o IdentityFile="${ETCDIR}"/id_ed25519 -o StrictHostKeyChecking=no "${USERNAME}@${REALM}" "${check}" >/dev/null 2>&1
        rc=$?
        if [ $rc -ne 0 ]; then
                #echo "[-]         Difinitly user is not exists: $bid: $rc"
                continue
        fi

        info=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t --set schema_name="${SCHEMA}" --set bid="${bid}" <<EOF
SELECT
        b.brigade_id,
        b.brigadier,
        b.created_at,
        d.deleted_at,
        d.reason
FROM
        :"schema_name".brigadiers AS b
LEFT JOIN :"schema_name".deleted_brigadiers AS d ON
                b.brigade_id=d.brigade_id
        WHERE
                b.brigade_id=:'bid'
EOF
)

        echo "Info: ${info}"

        "$(dirname "$0")"/delete_brigadier.sh "${bid}"

done