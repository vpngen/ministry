#!/bin/sh

set -e

printdef () {
        echo "Usage: $0 [options] command [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    add  <partner_id> <description>       # Add a partner key"
        echo "    del  <partner_id>                     # Delete a partner key"
        echo "    info <partner_id>                     # Show partner info"
        echo "    list                                  # List all partners"
        exit 1
}

addpartner () {
        partner_id="$1"
        partner_desc="$2"
        if [ "x" = "x${partner_id}" -o "x" = "x${partner_desc}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set partner_id=${partner_id} \
    --set desc=${partner_desc} <<EOF
BEGIN;
INSERT INTO :"schema_name".partners (partner_id,partner,is_active) VALUES (:'partner_id', :'desc', true);
COMMIT;
EOF
}

delpartner () {
        partner_id="$1"
        if [ "x" = "x${partner_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set partner_id=${partner_id} <<EOF
BEGIN;
        DELETE FROM :"schema_name".partners WHERE partner_id=:'partner_id';
COMMIT;
EOF
}

listpartners () {
        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} <<EOF
BEGIN;
        SELECT * FROM :"schema_name".partners;
COMMIT;
EOF
}

infopartner () {
        partner_id="$1"
        if [ "x" = "x${partner_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -v -a -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set partner_id=${partner_id} <<EOF
BEGIN;
        SELECT * FROM :"schema_name".partners WHERE partner_id=:'partner_id';
        SELECT CONCAT('    ',ecncode(key, 'base64') FROM :"schema_name".partners_keys WHERE partner_id=:'partner_id';
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
        echo "DBNAME and SCHEMA must be set"
        exit 1
fi

if [ "x" = "x$1" ]; then
        printdef
fi

opt="$1"
shift

case "$opt" in
        -h, --help)
                printdef
                ;;
        add)
                addpartner "$@"
                ;;
        del)
                delpartner "$@"
                ;;
        info)
                infopartner "$@"
                ;;
        list)
                listpartners "$@"
                ;;
        *)
                printdef
                ;;
esac

