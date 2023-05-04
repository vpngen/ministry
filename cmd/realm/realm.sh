#!/bin/sh

set -e

# !!! realm deletion is not implemented yet and subject to discussion

printdef () {
        echo "Usage: $0 [options] command [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    add  <realm_id> <description>       # Add a realm"
        echo "    activate <realm_id>                 # Activate a realm"
        echo "    deactivate <realm_id>               # Deactivate a realm"
        echo "    info <realm_id>                     # Show realm info"
        echo "    list                                # List all realms"
        exit 1
}


addrealm () {
        realm_id="$1"
        control_ip="$2"
        if [ "x" = "x${realm_id}" -o "x" = "x${control_ip}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set realm_id="${realm_id}" \
    --set control_ip="${control_ip}" <<EOF
BEGIN;
        INSERT INTO :"schema_name".realms (realm_id,control_ip,is_active) VALUES (:'realm_id', :'control_ip', false);
COMMIT;
EOF

        echo "Realm ${realm_id} added."
}

inforealm () {
        realm_id="$1"
        if [ "x" = "x${realm_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set realm_id=${realm_id} <<EOF
BEGIN;
        SELECT * FROM :"schema_name".realms WHERE realm_id=:'realm_id';
COMMIT;
EOF
}

listrealms () {
        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT * FROM :"schema_name".realms;
COMMIT;
EOF
}

activate () {
        realm_id="$1"
        if [ "x" = "x${realm_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name="${SCHEMA}" \
    --set realm_id="${realm_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".realms SET is_active=true WHERE realm_id=:'realm_id';
COMMIT;
EOF
        echo "Realm ${realm_id} activated"
}

deactivate () {
        realm_id="$1"
        if [ "x" = "x${realm_id}" ]; then
                printdef
        fi

        ON_ERROR_STOP=yes psql -qt -d "${DBNAME}" \
    --set schema_name"${SCHEMA}" \
    --set realm_id="${realm_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".realms SET is_active=false WHERE realm_id=:'realm_id';
COMMIT;
EOF

        echo "Realm ${realm_id} deactivated"
}

CONFDIR=${CONFDIR:-"/etc/vgdept"}
echo "confdir: ${CONFDIR}"
DBNAME=${DBNAME:-$(cat ${CONFDIR}/dbname)}
echo "dbname: $DBNAME"
SCHEMA=${SCHEMA:-$(cat ${CONFDIR}/schema)}
echo "schema: $SCHEMA"

if [ "x" = "x${DBNAME}" -o "x" = "x${SCHEMA}" ]; then
        echo "Error: DBNAME and SCHEMA must be set"
        exit 1
fi

if [ "x" = "x$1" ]; then
        printdef
fi

opt="$1"
shift

case "$opt" in
        add)
                addrealm "$@"
                ;;
        activate)
                activatedc "$@"
                ;;
        deactivate)
                deactivatedc "$@"
                ;;
        info)
                inforealm "$@"
                ;;
        list)
                listrealms "$@"
                ;;
        *)
                printdef
                ;;
esac