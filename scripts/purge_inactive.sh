#!/bin/sh

ppath="$0"
#exec_dir="$(dirname "${ppath}")"
app_name="$(basename "${ppath}")"

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}
REASON="inactive"

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

DAYS=${DAYS:-"7"}
NUMS=${NUMS:-"100000"}
MINACTIVE=${MINACTIVE:-"10"}

VIP_BRIGADES_FILE_HOME="${HOME}/.vip_brigades_files"
if [ -s "${VIP_BRIGADES_FILE_HOME}" ]; then
        VIP_BRIGADES_FILES="${VIP_BRIGADES_FILES} ${VIP_BRIGADES_FILE_HOME}"
fi

VIP_BRIGADES_FILE_ETC="/etc/vgdept/vip_brigades_files"
if [ -s "${VIP_BRIGADES_FILE_ETC}" ]; then
        VIP_BRIGADES_FILES="${VIP_BRIGADES_FILES} ${VIP_BRIGADES_FILE_ETC}"
fi



purge_per_igrp () {
        REALM="${1}"
        igroup="${2}"
        
        if [ -z "${REALM}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        if [ -z "${igroup}" ]; then
                echo "[!]         IGRP is empty"
                exit 1
        fi

        ICMD="getwasted inactive -x ${MINACTIVE} -d ${DAYS} -n ${NUMS} -igrp"
        echo "GET REALM: ${REALM} IGRP: ${igroup} WASTED: ${ICMD}"

        wasted=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${ICMD}" | grep ";${igroup}" | cut -d ';' -f 1)
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        # TODO: same code bi purge never visited
        for bid in ${wasted}; do
                #shellcheck disable=SC2086
                if grep -s -q -F "${bid}" ${VIP_BRIGADES_FILES}; then
                        echo "[-]         Brigade ${bid} is VIP"
                        continue
                fi

                echo "delete ${bid}"

                vip=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${bid}" \
                --set schema_name="${SCHEMA}" <<EOF
                SELECT 
                        COUNT(*)
                FROM 
                        :"schema_name".brigadier_vip
                WHERE 
                        brigade_id = :'brigade_id'
EOF
)
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with db: $rc"
                        continue
                fi

                if [ -n "${vip}" ] && [ "${vip}" -ne 0 ]; then
                        echo "[-]         Brigade is not ready for deletion. Is VIP."
                        continue
                fi

                count=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${bid}" \
                --set schema_name="${SCHEMA}" <<EOF
                SELECT 
                        COUNT(*)
                FROM 
                        :"schema_name".brigadier_realms
                WHERE 
                        brigade_id = :'brigade_id'
                        AND draft = false
                        AND featured = false;
EOF
)
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with db: $rc"
                        continue
                fi

                if [ -n "${count}" ] && [ "${count}" -ne 0 ]; then
                        echo "[-]         Brigade is not ready for deletion. Has ${count} active realms."
                        continue
                fi

                "$(dirname "$0")"/delete_brigadier.sh "${bid}" "${REASON}"

                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with deletion: $rc"
                        continue
                fi
        done
}

purge_per_realm () {
        REALM="${1}"
        
        if [ -z "${REALM}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        REALM_CMD="getwasted inactive -x ${MINACTIVE} -d ${DAYS} -n ${NUMS} -igrp"
        echo "GET REALM: ${REALM} WASTED: ${REALM_CMD}"

        igroups=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${REALM_CMD}" | cut -d ';' -f 2 | sort | uniq)
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        d="$(date +"%Y%m%d")"
        mkdir -p "${HOME}/logs/${d}"

        echo "${igroups}" | xargs --verbose -I {} -P 8 sh -c "${ppath}"' '"${REALM}"' {} 2>&1 | tee '"${HOME}/logs/${d}/${app_name}-"'{}-$(date +"%Y%m%d%H%M").logs'
}

REALM_IP="${1}"
IGRP_ID="${2}"

if [ -n "${REALM_IP}" ] && [ -n "${IGRP_ID}" ]; then
        purge_per_igrp "${REALM_IP}" "${IGRP_ID}"

        exit 0
fi

realms=$(psql -d "${DBNAME}" \
        -q -t -A --csv \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" << EOF
        SELECT 
                control_ip 
        FROM 
                :"schema_name".realms 
        WHERE 
                is_active = true;
EOF
)

for realm in ${realms}; do
        echo "[+]     Realm: ${realm}"
        purge_per_realm "${realm}" 
done