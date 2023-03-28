#!/bin/sh

set -e

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

        echo "token: ${token} name: ${name}     partner_id: ${partner_id}"

        if [ "x" = "x${partner_id}" -o "x" = "x${token}" -o "x" = "x${token_name}" ]; then
                printdef
        fi

        #token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
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
        if [ "x" = "x${partner_id}" -o "x" = "x${token}" ]; then
                printdef
        fi

        # token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
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

CONFDIR=${CONFDIR:-"/etc/vgdept"}
echo "confdir: ${CONFDIR}"
DBNAME=${DBNAME:-$(cat ${CONFDIR}/dbname)}
echo "dbname: $DBNAME"
SCHEMA=${SCHEMA:-$(cat ${CONFDIR}/schema)}
echo "schema: $SCHEMA"

if [ "x" = "x${DBNAME}" -o "x" = "x${SCHEMA}" ]; then
        echo "ERROR: DBNAME and SCHEMA must be set"
        exit 1
fi

if [ "x" = "x$1" ]; then
        printdef
fi

opt="$1";
shift

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
        *)
                printdef
                ;;
esac



