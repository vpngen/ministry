#!/bin/sh

SSHKEY=${SSHKEY:-"/etc/vgdept/id_ed25519"}
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"library"}
USERNAME=${USERNAME:-"_valera_"}
REASON=${REASON:-"manual_deletion"}

bid=${1}

if [ -z "${bid}" ]; then
        echo "Usage: $0 <brigade_id as UUID"
        exit 1
fi

REALM=$(psql -d "${DBNAME}" -q -t -A \
        --set ON_ERROR_STOP=yes \
        --set brigade_id="${bid}" \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT 
                r.realm_id 
        FROM
                :"schema_name".brigadiers b
                JOIN :"schema_name".realms r ON b.realm_id = r.realm_id
        WHERE 
                b.brigade_id = :'brigade_id';
COMMIT;
EOF
)
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with db: $rc"
        exit 1
fi

if [ -z "${REALM}" ]; then
        echo "[-]         Realm is empty"
        exit 1
fi

psql -d "${DBNAME}" -q -t -A \
        --set ON_ERROR_STOP=yes \
        --set brigade_id="${bid}" \
        --set reason="${REASON}" \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        INSERT INTO :"schema_name".deleted_brigadiers (brigade_id, reason) VALUES (:'brigade_id',:'reason') ON CONFLICT DO NOTHING;
COMMIT;
EOF
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with db: $rc"
        exit 1
fi

del="delbrigade -uuid ${bid}"
echo "${del}"

ssh -o IdentitiesOnly=yes -o IdentityFile="${SSHKEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${del}"
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with deletion: $rc"
        exit 1
fi
