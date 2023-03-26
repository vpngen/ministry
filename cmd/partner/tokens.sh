#!/bin/sh

set -e

printdef () {
        echo "Usage: $0 [options] command <partner_id> [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    addkey <partner_id>           # Add a partner key"
        echo "    delkey <partner_id> <key>     # Delete a partner key"
        exit 1
}

addkey () {
        partner_id="$1"
        key="$2"
        if [ "x" = "x${partner_id}" -o "x" = "x${key}" ]; then
                printdef
        fi

        # token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set partner_id=${partner_id} \
    --set key=${key} <<EOF
BEGIN;
        INSERT INTO 
                :"schema_name".partners_keys 
                (partner_id, key) 
        VALUES 
                (:'partner_id', decode(:'key', 'base64'));
COMMIT;
EOF

}

delkey () {
        partner_id="$1"
        key="$2"

        if [ "x" = "x${partner_id}" -o "x" = "x${key}" ]; then
                printdef
        fi

        # token='\x'$(echo "${key}" | base64 --decode | hexdump -ve '1/1 "%02x"')

        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set partner_id=${partner_id} \
    --set key=${key} <<EOF
BEGIN;

        DELETE FROM 
                :"schema_name".partners_keys 
        WHERE 
                partner_id=:'partner_id' 
                AND key=decode(:'key', 'base64');

COMMIT;
EOF

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
        -h, --help)
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



