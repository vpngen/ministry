#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

SSHKEY=${SSHKEY:-"/etc/vgdept/id_ed25519"}
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
                r.control_ip 
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

num=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSHKEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${del}")
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with deletion: $rc"
        exit 1
fi

echo "[+]         ${num} slots rest"

#psql -d "${DBNAME}" -q -t -A \
#        --set ON_ERROR_STOP=yes \
#        --set schema_name="${SCHEMA}" <<EOF
#        --set free_slots="${num}" \
#BEGIN;
#        UPDATE :"schema_name".realms SET free_slots = :free_slots WHERE control_ip = :'REALM';
#COMMIT;
#EOF
#rc=$?
#if [ $rc -ne 0 ]; then
#        echo "[-]         Something wrong with db: $rc"
#        exit 1
#fi