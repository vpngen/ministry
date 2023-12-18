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

bid="${1}"

if [ -z "${bid}" ]; then
        echo "Usage: $0 <brigade_id> [<reason>]"
        exit 1
fi

REASON="${2:-"manual_deletion"}"
ACTION="${3}"

l=$(printf "%s" "$bid" | wc -c)

if [ "$l" -eq 26 ]; then
        echo "[?]         Brigade ID: ${bid}"

        if ! bid=$(echo "${bid}=========" | base32 -d 2>/dev/null | hexdump -ve '1/1 "%02x"'); then
                echo "[-]         Brigade ID is invalid"
                exit 1
        fi
elif [ "$l" -ne 36 ]; then
        echo "[-]         Brigade ID is invalid"
        exit 1
fi

if [ -z "${ACTION}" ]; then
        realm_id=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${bid}" \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT 
                r.realm_id 
        FROM
                :"schema_name".brigadier_realms br
                JOIN :"schema_name".realms r ON br.realm_id = r.realm_id
        WHERE 
                br.brigade_id = :'brigade_id'
                AND br.draft = false
                AND br.featured = true
                AND (
                        SELECT 
                                COUNT(*)
                        FROM 
                                :"schema_name".brigadier_realms
                        WHERE 
                                brigade_id = :'brigade_id'
                                AND draft = false
                                AND featured = false
                ) = 0;
COMMIT;
EOF
        )
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with db: $rc"
                exit 1
        fi

        if [ -z "${realm_id}" ]; then
                echo "[-]         Brigade is not ready for deletion"
                exit 1
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

        flock -x -w "${LOCK_TIMEOUT}" "${spinlock}" "${0}" "${bid}" "${REASON}" "go"
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with deletion: $rc"
                exit 1
        fi

        exit 0
fi

if [ -n "${ACTION}" ] && [ "${ACTION}" != "go" ]; then
        echo "[-]         Action is invalid"
        exit 1
fi

control_ip=$(psql -d "${DBNAME}" -q -t -A \
        --set ON_ERROR_STOP=yes \
        --set brigade_id="${bid}" \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT 
                r.control_ip 
        FROM
                :"schema_name".brigadier_realms br
                JOIN :"schema_name".realms r ON br.realm_id = r.realm_id
        WHERE 
                br.brigade_id = :'brigade_id'
                AND br.draft = false
                AND br.featured = true
                AND (
                        SELECT 
                                COUNT(*)
                        FROM 
                                :"schema_name".brigadier_realms
                        WHERE 
                                brigade_id = :'brigade_id'
                                AND draft = false
                                AND featured = false
                ) = 0;
COMMIT;
EOF
        )
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with db: $rc"
        exit 1
fi
if [ -z "${control_ip}" ]; then
        echo "[-]         Brigade is not ready for deletion"
        exit 1
fi

del="delbrigade -uuid ${bid}"
echo "${del}"

num=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${control_ip}" "${del}")
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with realm: $rc"
        exit 1
fi

result="$(psql -d "${DBNAME}" -q -t -A \
        --set ON_ERROR_STOP=yes \
        --set brigade_id="${bid}" \
        --set reason="${REASON}" \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        INSERT INTO 
                :"schema_name".deleted_brigadiers 
                (brigade_id, reason, realm_id) 
        SELECT
                :'brigade_id',:'reason', br.realm_id
        FROM
                :"schema_name".brigadier_realms br
        WHERE 
                br.brigade_id = :'brigade_id'
                AND (
                        SELECT 
                                COUNT(*)
                        FROM 
                                :"schema_name".brigadier_realms
                        WHERE 
                                brigade_id = :'brigade_id'
                                AND draft = false
                                AND featured = false
                ) = 0;

        INSERT INTO 
                :"schema_name".brigadier_realms_actions
                (brigade_id, realm_id, event_name, event_info, event_time)
        SELECT
                :'brigade_id', br.realm_id, 'remove', '', now() AT TIME ZONE 'UTC'
        FROM
                :"schema_name".brigadier_realms br
        WHERE 
                br.brigade_id = :'brigade_id'
        AND (
                SELECT 
                        COUNT(*)
                FROM 
                        :"schema_name".brigadier_realms
                WHERE 
                        brigade_id = :'brigade_id'
                        AND draft = false
                        AND featured = false
        ) = 0;
        
        DELETE FROM 
                :"schema_name".brigadier_realms
        WHERE 
                brigade_id = :'brigade_id'
                AND draft = false
                AND featured = true
                AND (
                        SELECT 
                                COUNT(*)
                        FROM 
                                :"schema_name".brigadier_realms
                        WHERE 
                                brigade_id = :'brigade_id'
                                AND draft = false
                                AND featured = false
                ) = 0;

        INSERT INTO 
                :"schema_name".brigades_actions 
                (brigade_id, event_name, event_info, event_time) 
        SELECT
                :'brigade_id', 'delete_brigade', :'reason', now() AT TIME ZONE 'UTC'
        WHERE (
                SELECT 
                        COUNT(*)
                FROM 
                        :"schema_name".brigadier_realms
                WHERE 
                        brigade_id = :'brigade_id'
                        AND draft = false
                        AND featured = false
        ) = 0
        RETURNING :'brigade_id';
COMMIT;
EOF
)"
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong with db: $rc"
        exit 1
fi

if [ -z "${result}" ]; then
        echo "[-]         Brigade is not ready for deletion"
        exit 1
fi

echo "[+]         ${num} slots rest"
