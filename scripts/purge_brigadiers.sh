#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

DELETEDLIST=${DELETEDLIST:-"${HOME}/deleted_brigadiers.list"}

INTERVAL=${INTERVAL:-"1 mons"}
NUMS=${NUMS:-"10"}

psql -d "${DBNAME}" -t -A -q \
        --set schema_name="${SCHEMA}" \
        --set interval="${INTERVAL}" <<EOF | tee -a "${DELETEDLIST}"
BEGIN;
        SELECT 
                 NOW() AT TIME ZONE 'UTC',
                 b.brigadier, b.created_at, d.* 
        FROM 
                :"schema_name".deleted_brigadiers AS d 
        LEFT JOIN 
                :"schema_name".brigadiers AS b ON d.brigade_id=b.brigade_id 
        WHERE 
                d.deleted_at < NOW() AT TIME ZONE 'UTC' - interval :'interval';
COMMIT;
EOF

psql -d "${DBNAME}" -t -A \
        --set interval="${INTERVAL}" <<EOF
BEGIN;

DO \$purge\$ DECLARE r RECORD;

BEGIN

        FOR r IN SELECT 
                        d.brigade_id AS id
                FROM 
                        "${SCHEMA}".deleted_brigadiers AS d 
                WHERE 
                        d.deleted_at < NOW() AT TIME ZONE 'UTC' - interval '${INTERVAL}'
        LOOP
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadier_keys WHERE brigadier_keys.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadier_salts WHERE brigadier_salts.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".deleted_brigadiers WHERE deleted_brigadiers.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadiers WHERE brigadiers.brigade_id=' || quote_literal(r.id);
        END LOOP;

END\$purge\$;

COMMIT;
EOF



