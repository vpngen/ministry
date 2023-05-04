#!/bin/sh

ETCDIR="/etc/vgdept"
DBNAME=${DBNAME:-$(cat "${ETCDIR}"/dbname)}
SCHEMA=${SCHEMA:-$(cat "${ETCDIR}"/schema)}
USERNAME="_valera_"
REALM="10.10.100.252"
REASON="inactive"

DAYS=${DAYS:-"1"}
NUMS=${NUMS:-"1000"}
MIN_USERS=${MIN_USER:-"5"}

cmd="getwasted inactive -m ${DAYS} -n ${NUMS}"
echo "${cmd}"
wasted=$(ssh -o IdentitiesOnly=yes -o IdentityFile=${ETCDIR}/id_ed25519 -o StrictHostKeyChecking=no ${USERNAME}@${REALM} ${cmd})
rc=$?
if [ $rc -ne 0 ]; then
        echo "[-]         Something wrong: $rc"
        exit 1
fi

for bid in ${wasted}; do
        echo "delete ${bid}"

        c="psql -d ${DBNAME} -q -v ON_ERROR_STOP=yes -t -A --set brigade_id=${bid} --set reason=${REASON}"
        echo "${c}"

        ${c} <<EOF
        BEGIN;
                INSERT INTO ${SCHEMA}.deleted_brigadiers (brigade_id, reason) VALUES (:'brigade_id',:'reason') ON CONFLICT DO NOTHING;
                INSERT INTO ${SCHEMA}.brigades_actions (brigade_id, event_name, event_info, event_time) VALUES (:'brigade_id', 'delete_brigade', :'reason', now() AT TIME ZONE 'UTC');
        COMMIT;
EOF
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with db: $rc"
                continue
        fi

        del="delbrigade -uuid ${bid}"
        echo "${del}"

        ssh -o IdentitiesOnly=yes -o IdentityFile=${ETCDIR}/id_ed25519 -o StrictHostKeyChecking=no ${USERNAME}@${REALM} ${del}
        rc=$?
        if [ $rc -ne 0 ]; then
                echo "[-]         Something wrong with deletion: $rc"
                continue
        fi
done