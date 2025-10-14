#!/bin/sh

DBNAME=${DBNAME:-"vgdept"}
SCHEMA=${SCHEMA:-"head"}

DELETEDLIST=${DELETEDLIST:-"${HOME}/deleted_brigadiers.list"}

INTERVAL=${INTERVAL:-"2 mons"}

while true; do
        info=$(psql -d "${DBNAME}" -t -A -q \
                --set ON_ERROR_STOP=yes \
                --set schema_name="${SCHEMA}" \
                --set interval="${INTERVAL}" <<EOF
BEGIN;
        WITH selected_brigade AS (
                SELECT 
                        d.brigade_id AS id,
                        b.brigadier
                FROM 
                        :"schema_name".deleted_brigadiers AS d 
                        JOIN :"schema_name".brigadiers AS b ON d.brigade_id=b.brigade_id
                WHERE 
                        d.deleted_at < NOW() AT TIME ZONE 'UTC' - interval '${INTERVAL}'
                        AND (
                                SELECT 
                                        COUNT(*)
                                FROM 
                                        :"schema_name".brigadier_realms br
                                WHERE 
                                        br.brigade_id = d.brigade_id
                        ) = 0
                LIMIT 1
                FOR UPDATE
        ),
        brigade_info AS (
                SELECT 
                        NOW() AT TIME ZONE 'UTC', 
                        b.brigadier, 
                        b.created_at, 
                        d.deleted_at,
                        d.reason, 
                        r.realm_name
                FROM 
                        :"schema_name".deleted_brigadiers AS d
                        JOIN selected_brigade s ON d.brigade_id=s.id
                        LEFT JOIN :"schema_name".brigadiers AS b ON d.brigade_id=b.brigade_id
                        LEFT JOIN :"schema_name".realms AS r ON d.realm_id=r.realm_id
        ),
        delete_keys AS (
                DELETE FROM 
                        :"schema_name".brigadier_keys b
                USING
                        selected_brigade s
                WHERE 
                        b.brigade_id = s.id
        ),
        delete_salts AS (
                DELETE FROM 
                        :"schema_name".brigadier_salts b
                USING
                        selected_brigade s
                WHERE 
                        b.brigade_id = s.id
        ),
        delete_deleted_brigadiers AS (
                DELETE FROM 
                        :"schema_name".deleted_brigadiers b
                USING
                        selected_brigade s
                WHERE 
                        b.brigade_id = s.id
        ),
        delete_brigadiers AS (
                DELETE FROM 
                        :"schema_name".brigadiers b
                USING
                        selected_brigade s
                WHERE 
                        b.brigade_id = s.id
        ),
        update_brigadiers_ids AS (
                UPDATE 
                        :"schema_name".brigadiers_ids 
                SET 
                        purged_at = NOW() AT TIME ZONE 'UTC'
                FROM 
                        selected_brigade s
                WHERE 
                        brigade_id = s.id
        ),
        insert_brigades_actions AS (
                INSERT INTO 
                        :"schema_name".brigades_actions 
                        (brigade_id, event_name, event_info, event_time) 
                SELECT
                        id,
                        'purge_brigade', 
                        'expired', 
                        NOW() AT TIME ZONE 'UTC'
                FROM
                        selected_brigade
        )
        SELECT * FROM brigade_info;
COMMIT;
EOF
)

        if [ -z "${info}" ]; then
                break
        fi

        echo "${info}" | tee -a "${DELETEDLIST}"
done