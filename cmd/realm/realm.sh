#!/bin/sh

set -e

SSHKEY=${SSHKEY:-"/etc/vgdept/id_ed25519"}
REMOTE_REALMNAME_FILE=${REMOTE_REALMNAME_FILE:-"/etc/vg-dc-mgmt/dc-name.txt"} # "realm_id,realm_name"
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

# !!! realm deletion is not implemented yet and subject to discussion

printdef () {
        echo "Usage: $0 [options] command [command args and options]"
        echo "Options:"
        echo "    -h     Print this help message"       
        echo "Commands:"
        echo "    add  <realm_id> <realm_name> <description>    # Add a realm"
        echo "    activate <realm_id>                           # Activate a realm"
        echo "    deactivate <realm_id>                         # Deactivate a realm"
        echo "    info <realm_id>                               # Show realm info"
        echo "    list                                          # List all realms"
        exit 1
}

add_dc () {
        realm_id="$1"
        realm_name="$2"
        control_ip="$3"
        if [ -z "${realm_id}" ] || [ -z "${realm_name}" ] || [ "${control_ip}" ]; then
                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set realm_id="${realm_id}" \
        --set realm_name="${realm_name}" \
        --set control_ip="${control_ip}" <<EOF
BEGIN;
        INSERT INTO :"schema_name".realms (realm_id,realm_name,control_ip,is_active,update_time) VALUES (:'realm_id', :'realm_name', :'control_ip', false, NOW());
COMMIT;
EOF
        echo "Realm ${realm_id} added."
}

info_dc () {
        realm_id="$1"
        if [ -z "${realm_id}" ]; then
                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        SELECT * FROM :"schema_name".realms WHERE realm_id=:'realm_id';
COMMIT;
EOF
}

list_dc () {
        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT * FROM :"schema_name".realms;
COMMIT;
EOF
}

activate_dc () {
        realm_id="$1"
        if [ -z "${realm_id}" ]; then
                printdef
        fi

        realm_name=$(psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        SELECT realm_name FROM :"schema_name".realms WHERE realm_id=:'realm_id';
COMMIT;
EOF
)

        if [ -z "${realm_name}" ]; then
                echo "Error: realm ${realm_id} not found"
                exit 1
        fi

        echo "Realm name: ${realm_name}"

        cmd="cat > ${REMOTE_REALMNAME_FILE}"
        echo -n "${realm_id},${realm_name}" | ssh -o IdentitiesOnly=yes -o IdentityFile="${SSHKEY}" -o StrictHostKeyChecking=no "${USERNAME}"@"${REALM}" "${cmd}"

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".realms SET is_active=true, update_time=NOW() WHERE realm_id=:'realm_id';
COMMIT;
EOF

        echo "Realm ${realm_id} activated"
}

deactivate_dc () {
        realm_id="$1"
        if [ -z "${realm_id}" ]; then
                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name"${SCHEMA}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        UPDATE :"schema_name".realms SET is_active=false, update_time=NOW() WHERE realm_id=:'realm_id';
COMMIT;
EOF

        echo "Realm ${realm_id} deactivated"
}

opt="$1"
shift

if [ -z "${opt}" ]; then
        printdef
fi

case "$opt" in
        add)
                add_dc "$@"
                ;;
        activate)
                activate_dc "$@"
                ;;
        deactivate)
                deactivate_dc "$@"
                ;;
        info)
                info_dc "$@"
                ;;
        list)
                list_dc "$@"
                ;;
        *)
                printdef
                ;;
esac