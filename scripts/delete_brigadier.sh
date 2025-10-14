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

VIP_BRIGADES_FILE_HOME="${HOME}/.vip_brigades_files"
if [ -s "${VIP_BRIGADES_FILE_HOME}" ]; then
        VIP_BRIGADES_FILES="${VIP_BRIGADES_FILES} ${VIP_BRIGADES_FILE_HOME}"
fi

VIP_BRIGADES_FILE_ETC="/etc/vgdept/vip_brigades_files"
if [ -s "${VIP_BRIGADES_FILE_ETC}" ]; then
        VIP_BRIGADES_FILES="${VIP_BRIGADES_FILES} ${VIP_BRIGADES_FILE_ETC}"
fi

bid="${1}"

if [ -z "${bid}" ]; then
        echo "Usage: $0 <brigade_id> [<reason>]"
        exit 1
fi

#shellcheck disable=SC2086
if grep -s -q -F "${bid}" ${VIP_BRIGADES_FILES}; then
        echo "[-]         Brigade ${bid} is VIP"
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

is_deletebale() {
        brigade_id="${1}"

        result=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${brigade_id}" <<EOF
        SELECT 
                bv.brigade_id
        FROM 
                head.brigadier_vip bv
        WHERE 
                bv.brigade_id = :'brigade_id'
        ;
EOF
        )

        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-][is deleteable] Something wrong with db: $rc"
                return 1
        fi

        if [ -n "${result}" ]; then
                echo "[-]         Brigade ${brigade_id} is VIP"
                return 1
        fi

        result=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${brigade_id}" <<EOF
        SELECT 
                brigade_id
        FROM 
                head.deleted_brigadiers
        WHERE 
                brigade_id = :'brigade_id'
        ;
EOF
        )

        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-][is deleteable] Something wrong with db: $rc"
                return 1
        fi

        if [ -n "${result}" ]; then
                echo "[-]         Brigade ${brigade_id} is already deleted"
                return 1
        fi
}

getrealm() {
        brigade_id="${1}"

        result=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${brigade_id}" \
                --set schema_name="${SCHEMA}" <<EOF
        SELECT
                COUNT(*) AS total_count,
	        COUNT(CASE WHEN br.draft = TRUE THEN 1 END) AS draft_count,
                COUNT(CASE WHEN br.featured = TRUE THEN 1 END) AS featured_count,
                MAX(CASE WHEN br.featured = TRUE THEN br.realm_id::text ELSE NULL END)::uuid AS realm_id,
                MAX(CASE WHEN br.featured = TRUE THEN r.control_ip::text ELSE NULL END)::inet AS control_ip,
                (
                        SELECT 
                                bra.realm_id
                        FROM 
                                :"schema_name".brigadier_realms_actions bra
                        WHERE 
                                bra.brigade_id = :'brigade_id'
                        ORDER BY bra.event_time DESC
                        LIMIT 1
                ) AS last_realm_id
        FROM
                :"schema_name".brigadier_realms br
        JOIN 	:"schema_name".realms r ON br.realm_id = r.realm_id
        LEFT JOIN  :"schema_name".brigadier_vip bv ON br.brigade_id = bv.brigade_id 
        WHERE
                br.brigade_id = :'brigade_id'
                AND bv.brigade_id IS NULL
        ;
EOF
)
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-][get realm] Something wrong with db: $rc"
                exit 1
        fi 

        total_count=$(echo "${result}" | cut -d '|' -f 1)
        draft_count=$(echo "${result}" | cut -d '|' -f 2)
        featured_count=$(echo "${result}" | cut -d '|' -f 3)
        realm_id=$(echo "${result}" | cut -d '|' -f 4)
        control_ip=$(echo "${result}" | cut -d '|' -f 5)
        last_realm_id=$(echo "${result}" | cut -d '|' -f 6)
}

if [ -z "${ACTION}" ]; then
        if ! is_deletebale "${bid}"; then
                exit 1
        fi

        getrealm "${bid}"

        if [ "${total_count}" -gt 1 ]; then
                echo "[-] Brigade is not ready for deletion. Realms total=${total_count}, draft=${draft_count}, featured=${featured_count}"
                exit 1
        fi

        if [ "${total_count}" -gt 0 ] && [ "${featured_count}" -eq 0 ]; then
                echo "[-] Brigade is not ready for deletion. No featured realms. Realms total=${total_count}, draft=${draft_count}"
                exit 1
        fi

        #if [ -z "${realm_id}" ]; then
        #        realm_id="00000000-0000-0000-0000-000000000000"
        #fi

        #spinlock_filename="${realm_id}.lock"
        #if [ -d "${HOME}" ] && [ -w "${HOME}" ]; then
        #        spinlock="${HOME}/${spinlock_filename}"
        #elif [ -d "/tmp" ] && [ -w "/tmp" ]; then
        #        spinlock="/tmp/${spinlock_filename}"
        #else
        #        echo "[-] Can't create spinlock file"
        #        exit 1
        #fi

        #flock -x -w "${LOCK_TIMEOUT}" "${spinlock}" "${0}" "${bid}" "${REASON}" "go"
        "${0}" "${bid}" "${REASON}" "go"
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-] Something wrong with deletion: $rc"
                exit 1
        fi

        exit 0
fi

if [ -n "${ACTION}" ] && [ "${ACTION}" != "go" ]; then
        echo "[-] Action is invalid"
        exit 1
fi

if ! is_deletebale "${bid}"; then
        exit 1
fi

getrealm "${bid}"

if [ -n "${realm_id}" ]; then
        if [ -z "${control_ip}" ]; then
                echo "[-] Brigade is not ready for deletion. No control_ip. Realm: ${realm_id}"
                exit 1
        fi

        del="delbrigade -uuid ${bid}"
        echo "${del}"

        num=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${control_ip}" "${del}" 2>&1)
        rc=$?
        if [ $rc -ne 0 ]; then
                # shellcheck disable=SC2143
                if [ -z "$(echo "${num}" | grep "Can't get control ip" | grep "no rows in result set")" ]; then
                        echo "[-] Something wrong with realm [ssh]: $rc"
                        if [ -n "${num}" ]; then
                                echo "[D] SSH DEBUG: ${num}"
                        fi

                        exit 1
                fi

                echo "[+] Brigade not found. Don't worry."
        fi
fi

if [ -z "${realm_id}" ]; then
        if [ -z "${last_realm_id}" ]; then
                echo "[-] Brigade is not ready for deletion. No realm_id nor last_realm_id"
                exit 1
        fi
fi

result="$(psql -d "${DBNAME}" -q -t -A \
        --set ON_ERROR_STOP=yes \
        --set brigade_id="${bid}" \
        --set realm_id="${realm_id}" \
        --set last_realm_id="${last_realm_id}" \
        --set reason="${REASON}" \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        INSERT INTO 
                :"schema_name".deleted_brigadiers 
                (brigade_id, reason, realm_id) 
        SELECT
                :'brigade_id',
                :'reason', 
                CASE WHEN br.realm_id IS NULL THEN :'last_realm_id'::uuid ELSE br.realm_id END AS realm_id
        FROM
                :"schema_name".brigadiers b
                LEFT JOIN :"schema_name".brigadier_realms br ON b.brigade_id = br.brigade_id
        WHERE 
                b.brigade_id = :'brigade_id'
                AND (
                        SELECT 
                                be.event_name
                        FROM 
                                :"schema_name".brigades_actions be
                        WHERE 
                                be.brigade_id = :'brigade_id'
                        ORDER BY be.event_time DESC
                        LIMIT 1
                ) IN ('create_brigade', 'restore_brigade');

        INSERT INTO 
                :"schema_name".brigadier_realms_actions
                (brigade_id, realm_id, event_name, event_info, event_time)
        SELECT
                :'brigade_id', br.realm_id, 'remove', '', now() AT TIME ZONE 'UTC'
        FROM
                :"schema_name".brigadier_realms br
        WHERE 
                br.brigade_id = :'brigade_id'
                AND br.realm_id = NULLIF(:'realm_id', '')::uuid;
        
        DELETE FROM 
                :"schema_name".brigadier_realms
        WHERE 
                brigade_id = :'brigade_id'
                AND realm_id = NULLIF(:'realm_id', '')::uuid;

        INSERT INTO 
                :"schema_name".brigades_actions 
                (brigade_id, event_name, event_info, event_time) 
        SELECT
                :'brigade_id', 'delete_brigade', :'reason', now() AT TIME ZONE 'UTC'
        FROM
                :"schema_name".brigadiers
        WHERE   
                brigade_id = :'brigade_id'              
                AND (
                        SELECT 
                                be.event_name
                        FROM 
                                :"schema_name".brigades_actions be
                        WHERE 
                                be.brigade_id = :'brigade_id'
                        ORDER BY be.event_time DESC
                        LIMIT 1
                ) IN ('create_brigade', 'restore_brigade')
        RETURNING brigade_id;
COMMIT;
EOF
)"
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-] Something wrong with db while deleting brigade: $rc."
        exit 1
fi

if [ -z "${result}" ]; then
        echo "[-] Brigade is not ready for deletion"
        exit 1
fi

if [ -n "${num}" ]; then
        case $num in
                ''|*[!0-9]*) ;;
                *) echo "[+]         ${num} slots rest" ;;
        esac
fi