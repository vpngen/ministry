#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

DELETEDLIST=${DELETEDLIST:-"${HOME}/deleted_brigadiers.list"}

INTERVAL=${INTERVAL:-"1 mons"}
NUMS=${NUMS:-"10"}

psql -d "${DBNAME}" -t -A -q \
        --set schema_name="${SCHEMA}" \
        --set interval="${INTERVAL}" <<EOF | tee -a "${DELETEDLIST}"
        SELECT 
                 NOW() AT TIME ZONE 'UTC',
                 b.brigadier, b.created_at, d.* 
        FROM 
                :"schema_name".deleted_brigadiers AS d 
        LEFT JOIN 
                :"schema_name".brigadiers AS b ON d.brigade_id=b.brigade_id 
        WHERE 
                d.deleted_at < NOW() AT TIME ZONE 'UTC' - interval :'interval'
                AND (
                        SELECT 
                                COUNT(*)
                        FROM 
                                :"schema_name".brigadier_realms br
                        WHERE 
                                br.brigade_id = d.brigade_id
                ) = 0;
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
                        AND (
                                SELECT 
                                        COUNT(*)
                                FROM 
                                        "${SCHEMA}".brigadier_realms br
                                WHERE 
                                        br.brigade_id = d.brigade_id
                ) = 0
        LOOP
                EXECUTE 'SELECT NOW() AT TIME ZONE ''UTC'', b.brigadier, b.created_at, d.* FROM "${SCHEMA}".deleted_brigadiers AS d LEFT JOIN "${SCHEMA}".brigadiers AS b ON d.brigade_id=b.brigade_id';
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadier_keys WHERE brigadier_keys.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadier_salts WHERE brigadier_salts.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".deleted_brigadiers WHERE deleted_brigadiers.brigade_id=' || quote_literal(r.id);
                EXECUTE 'DELETE FROM "${SCHEMA}".brigadiers WHERE brigadiers.brigade_id=' || quote_literal(r.id);
                EXECUTE 'UPDATE head.brigadiers_ids SET purged_at = NOW() AT TIME ZONE ''UTC'' WHERE brigade_id = ' || quote_literal(r.id);
                EXECUTE 'INSERT INTO "${SCHEMA}".brigades_actions (brigade_id, event_name, event_info, event_time) VALUES (' || quote_literal(r.id) || ', ''purge_brigade'', ''expired'', NOW() AT TIME ZONE ''UTC'')';
        END LOOP;

END\$purge\$;

COMMIT;
EOF

