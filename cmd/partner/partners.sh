#!/bin/sh

set -e

# !!! partner deletion is not implemented yet and subject to discussion

printdef () {
        echo "Usage: $0 [options] command [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    add  <partner_id> <description>       # Add a partner"
        echo "    info <partner_id>                     # Show partner info"
        echo "    list                                  # List all partners" 
        echo "    activate <partner_id>                 # Activate a partner"
        echo "    deactivate <partner_id>               # Deactivate a partner"
        echo "    attachdc <partner_id> <realm_id>      # Attach a partner to a realm"
        echo "    detachdc <partner_id> <realm_id>      # Detach a partner from a realm"
        exit 1
}

addpartner () {
        partner_id="$1"
        partner_desc="$2"
        if [ "x" = "x${partner_id}" -o "x" = "x${partner_desc}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -v -a -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" \
    --set desc="${partner_desc}" <<EOF
BEGIN;
INSERT INTO :"schema_name".partners (partner_id,partner,is_active) VALUES (:'partner_id', :'desc', false);
COMMIT;
EOF

        echo "Partner ${partner_id} added."
}

listpartners () {
        ON_ERROR_STOP=yes psql -v -a -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" <<EOF
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

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" <<EOF
BEGIN;
        SELECT 'Partner :' AS head, * FROM :"schema_name".partners WHERE partner_id=:'partner_id';
        SELECT CONCAT('    key: ',translate(encode(token, 'base64'),'+/=','-_'), ':', name) FROM :"schema_name".partners_tokens WHERE partner_id=:'partner_id';
        SELECT CONCAT('    realm: ',realm_id) FROM :"schema_name".partners_realms WHERE partner_id=:'partner_id';
COMMIT;
EOF
}

activate () {
        partner_id="$1"
        if [ "x" = "x${partner_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".partners SET is_active=true WHERE partner_id=:'partner_id';
COMMIT;
EOF

        echo "Partner ${partner_id} activated."
}

deactivate () {
        partner_id="$1"
        if [ "x" = "x${partner_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".partners SET is_active=false WHERE partner_id=:'partner_id';
COMMIT;
EOF

        echo "Partner ${partner_id} deactivated."
}

attachdc () {
        partner_id="$1"
        realm_id="$2"
        if [ "x" = "x${partner_id}" -o "x" = "x${realm_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" \
    --set realm_id="${realm_id}" <<EOF
BEGIN;
        INSERT INTO :"schema_name".partners_realms (partner_id,realm_id) VALUES (:'partner_id', :'realm_id');
COMMIT;
EOF
}

detachdc () {
        partner_id="$1"
        realm_id="$2"
        if [ "x" = "x${partner_id}" -o "x" = "x${realm_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set partner_id="${partner_id}" \
    --set realm_id="${realm_id}" <<EOF
BEGIN;
        DELETE FROM :"schema_name".partners_realms WHERE partner_id=:'partner_id' AND realm_id=:'realm_id';
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
        -h,--help)
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
        activate)
                activate "$@"
                ;;
        deactivate)
                deactivate "$@"
                ;;
        attachdc)
                attachdc "$@"
                ;;
        detachdc)
                detachdc "$@"
                ;;
        *)
                printdef
                ;;
esac

