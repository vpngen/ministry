#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}
REASON="empty"

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


wasted=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT 
                b.brigade_id
        FROM 
                :"schema_name".brigadiers AS b 
                LEFT JOIN  :"schema_name".brigadier_realms AS r ON b.brigade_id=r.brigade_id 
                LEFT JOIN  :"schema_name".deleted_brigadiers AS d ON b.brigade_id = d.brigade_id 
        WHERE 
                d.brigade_id IS NULL 
                AND r.brigade_id IS NULL
                AND (
                        SELECT 
                                be.event_name
                        FROM 
                                "head".brigades_actions be
                        WHERE 
                                be.brigade_id = b.brigade_id
                        ORDER BY be.event_time DESC
                        LIMIT 1
                ) IN ('create_brigade', 'restore_brigade');
COMMIT;
EOF
)

for bid in ${wasted}; do
        #shellcheck disable=SC2086
        if grep -s -q -F "${bid}" ${VIP_BRIGADES_FILES}; then
                echo "[-]         Brigade ${bid} is VIP"
                continue
        fi

        echo "delete ${bid}"
        
        "$(dirname "$0")"/delete_brigadier.sh "${bid}" "${REASON}"
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with deletion: $rc"
                continue
        fi
done

