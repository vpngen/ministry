#!/bin/sh

set -e

CONFDIR=${CONFDIR:-"/etc/vgdept"}
DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"library"}

echo "CONFDIR: ${CONFDIR}"
echo "DBNAME: ${DBNAME}"
echo "SCHEMA: ${SCHEMA}"

if [ -z "${DBNAME}" ] || [ -z "${SCHEMA}" ]; then
        echo "Error: DBNAME and SCHEMA must be set"
        exit 1
fi

printdef () {
        echo "Usage: $0 [options] command <partner_id> [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    addkey <partner_id> <token> <token_name>      # Add a partner token"
        echo "    delkey <partner_id> <token_name>              # Delete a partner token"
        exit 1
}

addkey () {
        partner_id="$1"
        token="$2"
        token_name="$3"

        token=$(echo -n "${token}" | basenc -d --base64url | basenc --base64 -w 0)

        echo "token: ${token} name: ${token_name}     partner_id: ${partner_id}"

        if [ -z "${partner_id}" ] || [ -z "${token}" ] || [ -z "${token_name}" ]; then
                printdef
        fi

        #token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" \
        --set token="${token}" \
        --set token_name="${token_name}" <<EOF
BEGIN;
        INSERT INTO 
                :"schema_name".partners_tokens 
                (partner_id, token, name) 
        VALUES 
                (:'partner_id', decode(:'token', 'base64'), :'token_name');
COMMIT;
EOF

        echo "Added token ${token} name ${token_name} for partner ${partner_id}"
}

delkey () {
        partner_id="$1"
        token_name="$2"
        if [ -z "${partner_id}" ] || [ -z "${token}" ]; then
                printdef
        fi

        # token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" \
        --set token_name="${token_name}" <<EOF
BEGIN;
        DELETE FROM 
                :"schema_name".partners_tokens 
        WHERE 
                partner_id=:'partner_id' 
                AND name=decode(:'token_name', 'base64');
COMMIT;
EOF

        echo "Deleted token ${token_name} for partner ${partner_id}"
}

listkeys () {
        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        SELECT 
        CONCAT('    token: ',translate(encode(t.token, 'base64'),'+/=','-_'), ':', t.name, ' pid: ', p.partner_id, ' name: "', p.partner, '" status: ', p.is_active)
        FROM 
                :"schema_name".partners_tokens AS t
                LEFT JOIN :"schema_name".partners AS p ON p.partner_id=t.partner_id;
COMMIT;
EOF
}

opt="$1";
shift

if [ -z "${opt}" ]; then
        printdef
fi


case "$opt" in
        -h,--help)
                printdef
                ;;
        addkey)
                addkey "$@"
                ;;
        delkey)
                delkey "$@"
                ;;
        listkeys)
                listkeys "$@"
                ;;
        *)
                printdef
                ;;
esac



