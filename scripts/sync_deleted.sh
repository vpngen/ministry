#!/bin/sh

ETCDIR="${HOME}/.ssh"
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}
USERNAME="_valera_"

LOCK_TIMEOUT=${LOCK_TIMEOUT:-"120"} # seconds

if [ $# -eq 1 ]; then
        info=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t --set schema_name="${SCHEMA}" --set bid="${bid}" <<EOF
SELECT
        r.control_ip,
        b.brigade_id,
        b.brigadier,
        b.created_at,
        d.deleted_at,
        d.reason
FROM
        :"schema_name".brigadiers AS b
        LEFT JOIN :"schema_name".deleted_brigadiers AS d ON b.brigade_id=d.brigade_id
        JOIN :"schema_name".realms r ON d.realm_id=r.realm_id
WHERE
        b.brigade_id=:'bid'
EOF
)

        if [ -z "${info}" ]; then
                echo "[-]         Brigade ID is invalid"
                exit 1
        fi

        control_ip=$(echo "${info}" | cut -f 1 -d "|" | tr -d '[:space:]')

        echo "Info: ${info}"

        del="delbrigade -uuid ${bid}"
        echo "${del}"

        ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${control_ip}" "${del}"
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with realm: $rc"
                exit 1
        fi

        exit 0
fi


list=$(psql -d "${DBNAME}" -v ON_ERROR_STOP=yes -t -A --set schema_name="${SCHEMA}" <<EOF
SELECT
        d.brigade_id, r.realm_id, r.control_ip
FROM
        :"schema_name".deleted_brigadiers d
        JOIN :"schema_name".realms r ON d.realm_id=r.realm_id
EOF
)


for line in ${list} ; do
        bid=$(echo "${line}" | cut -f 1 -d "|")
        realm_id=$(echo "${line}" | cut -f 2 -d "|")
        control_ip=$(echo "${line}" | cut -f 3 -d "|")

        check="checkbrigade -uuid ${bid}"

        # TODO: check strict error
        ssh -o IdentitiesOnly=yes -o IdentityFile="${ETCDIR}"/id_ed25519 -o StrictHostKeyChecking=no "${USERNAME}@${control_ip}" "${check}" >/dev/null 2>&1
        rc=$?
        if [ $rc -ne 0 ]; then
                #echo "[-]         Difinitly user is not exists: $bid: $rc"
                continue
        fi
        
        spinlock_filename="${realm_id}.lock"
        if [ -d "${HOME}" ] && [ -w "${HOME}" ]; then
                spinlock="${HOME}/${spinlock_filename}"
        elif [ -d "/tmp" ] && [ -w "/tmp" ]; then
                spinlock="/tmp/${spinlock_filename}"
        else
                echo "[-]         Can't create spinlock file"
                exit 1
        fi

        flock -x -w "${LOCK_TIMEOUT}" "${spinlock}" "${0}" "${bid}"
done