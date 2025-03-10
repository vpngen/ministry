#!/bin/sh

set -e

# !!! partner deletion is not implemented yet and subject to discussion

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

echo "DBNAME: ${DBNAME}"
echo "SCHEMA: ${SCHEMA}"

if [ -z "${DBNAME}" ] || [ -z "${SCHEMA}" ]; then
        echo "Error: DBNAME and SCHEMA must be set"
        exit 1
fi

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
        echo "    regson <partner_id>                   # Allow new registrations for a partner"
        echo "    regsoff <partner_id>                  # Disallow new registrations for a partner"
        echo "    attachdc <partner_id> <realm_id>      # Attach a partner to a realm"
        echo "    detachdc <partner_id> <realm_id>      # Detach a partner from a realm"
        exit 1
}

addpartner () {
        partner_id="$1"
        partner_desc="$2"

        if [ -z "${partner_id}" ] || [ -z "${partner_desc}" ]; then
                echo "Error: partner_id and description must be set ($*)" >&2

                printdef
        fi

        psql -v -a -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" \
        --set desc="${partner_desc}" <<EOF
BEGIN;
INSERT INTO 
        :"schema_name".partners 
        (partner_id,partner,is_active,open_for_regs,update_time) 
VALUES 
        (:'partner_id', :'desc', false, false, NOW() AT TIME ZONE 'UTC');
COMMIT;
EOF

        echo "Partner ${partner_id} added."
}

listpartners () {
        psql -v -a -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" <<EOF
BEGIN;
        SELECT * FROM :"schema_name".partners;
COMMIT;
EOF
}

infopartner () {
        partner_id="$1"
        if [ -z "${partner_id}" ]; then
                echo "Error: partner_id must be set" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        SELECT 
                'Partner :' AS head, * 
        FROM 
                :"schema_name".partners 
        WHERE 
                partner_id=:'partner_id';

        SELECT 
                CONCAT('    token: ',translate(encode(token, 'base64'),'+/=','-_'), ':', name) 
        FROM 
                :"schema_name".partners_tokens 
        WHERE 
                partner_id=:'partner_id';

        SELECT 
                CONCAT('    realm: ', r.realm_id, '  name: ', r.realm_name, '  active: ', r.is_active, '  slots: ',r.free_slots)
        FROM 
                :"schema_name".partners_realms pr
        JOIN 
                :"schema_name".realms r ON pr.realm_id=r.realm_id 
        WHERE 
                partner_id=:'partner_id';
COMMIT;
EOF
}

activate () {
        partner_id="$1"
        if [ -z "${partner_id}" ]; then
                echo "Error: partner_id must be set" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE 
                :"schema_name".partners 
        SET 
                is_active=true, update_time=NOW() AT TIME ZONE 'UTC'
        WHERE 
                partner_id=:'partner_id';
COMMIT;
EOF

        echo "Partner ${partner_id} activated."
}

deactivate () {
        partner_id="$1"
        if [ -z "${partner_id}" ]; then
                echo "Error: partner_id must be set" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE 
                :"schema_name".partners 
        SET 
                is_active=false, update_time=NOW() AT TIME ZONE 'UTC'
        WHERE 
                partner_id=:'partner_id';
COMMIT; 
EOF

        echo "Partner ${partner_id} deactivated."
}

regson () {
        partner_id="$1"
        if [ -z "${partner_id}" ]; then
                echo "Error: partner_id must be set" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE 
                :"schema_name".partners 
        SET 
                open_for_regs=true, update_time=NOW() AT TIME ZONE 'UTC'
        WHERE 
                partner_id=:'partner_id';
COMMIT;
EOF

        echo "Partner ${partner_id} open for new registrations."
}

regsoff () {
        partner_id="$1"
        if [ -z "${partner_id}" ]; then
                echo "Error: partner_id must be set" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" <<EOF
BEGIN;
        UPDATE 
                :"schema_name".partners 
        SET 
                open_for_regs=false, update_time=NOW() AT TIME ZONE 'UTC'
        WHERE 
                partner_id=:'partner_id';
COMMIT;
EOF

        echo "Partner ${partner_id} closed for new registrations."
}

attachdc () {
        partner_id="$1"
        realm_id="$2"
        if [ -z "${partner_id}" ] || [ -z "${realm_id}" ]; then
                echo "Error: partner_id and realm_id must be set ($*)" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        INSERT INTO 
                :"schema_name".partners_realms 
                (partner_id,realm_id) 
        VALUES 
                (:'partner_id', :'realm_id');
COMMIT;
EOF
}

detachdc () {
        partner_id="$1"
        realm_id="$2"
        if [ -z "${partner_id}" ] || [ -z "${realm_id}" ]; then
                echo "Error: partner_id and realm_id must be set ($*)" >&2

                printdef
        fi

        psql -qt -d "${DBNAME}" \
        --set ON_ERROR_STOP=yes \
        --set schema_name="${SCHEMA}" \
        --set partner_id="${partner_id}" \
        --set realm_id="${realm_id}" <<EOF
BEGIN;
        DELETE FROM 
                :"schema_name".partners_realms 
        WHERE 
                partner_id=:'partner_id' 
        AND 
                realm_id=:'realm_id';
COMMIT;
EOF
}

opt="$1"
if [ -z "${opt}" ]; then
        echo "Error: command must be specified" >&2

        printdef
fi

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
        regson)
                regson "$@"
                ;;
        regsoff)
                regsoff "$@"
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
