#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

USERNAME=${USERNAME:-"_valera_"}
REASON="never_visited"

DAYS=${DAYS:-"1"}
NUMS=${NUMS:-"100"}

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

CMD="getwasted notvisited -d ${DAYS} -n ${NUMS}"
echo "GET WASTED: ${CMD}"

purge_per_realm () {
        REALM="${1}"
        realm_id="${2}"

        if [ -z "${REALM}" ]; then
                echo "[!]         Realm is empty"
                exit 1
        fi

        wasted=$(ssh -o IdentitiesOnly=yes -o IdentityFile="${SSH_KEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${CMD}")
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong: $rc"
                exit 1
        fi

        # TODO: same code bi purge inactive
        for bid in ${wasted}; do
                echo "delete ${bid}"

                count=$(psql -d "${DBNAME}" -q -t -A \
                --set ON_ERROR_STOP=yes \
                --set brigade_id="${bid}" \
                --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT 
                COUNT(*)
        FROM 
                :"schema_name".brigadier_realms
        WHERE 
                brigade_id = :'brigade_id'
                AND draft = false
                AND featured = false
COMMIT;
EOF
)
                rc=$?
                if [ $rc -ne 0 ]; then
                        echo "[-]         Something wrong with db: $rc"
                        continue
                fi

                if [ -n "${count}" ] && [ "${count}" -ne 0 ]; then
                        echo "[-]         Brigade is not ready for deletion"
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

realms=$(psql -d "${DBNAME}" \
        -q -t -A --csv \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" << EOF
        SELECT 
                realm_id,control_ip 
        FROM 
                :"schema_name".realms 
        WHERE 
                is_active = true;
EOF
)

for realm in ${realms}; do
        realm_id=$(echo "${realm}" | cut -d ',' -f 1)
        control_ip=$(echo "${realm}" | cut -d ',' -f 2)

        echo "[+]     Realm: ${realm_id} control_ip: ${control_ip}"
        purge_per_realm "${control_ip}" "${realm_id}"
done
