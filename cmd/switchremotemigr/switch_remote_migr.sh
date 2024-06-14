#!/bin/sh

set -e

DBNAME=${DBNAME:-"vgdept"}

SCHEMA=${PAIRS_SCHEMA:-"head"}

printdef() {
        echo "Usage: <command> <options>" 
        echo "  Commands:"
        echo "          create - create spare brigades in the datacenter"
        echo "          Options:"
        echo "                  -n : dry run"
        echo "                  -t <datacenter_id> : target datacenter"
        echo "                  -f <snapshot_file> : snapshot file (may be prepared)"
        echo "          switch - promote brigades to the datacenter"
        echo "          Options:"
        echo "                  -n : dry run"
        echo "                  -t <datacenter_id> : target datacenter"
        echo "                  -f <snapshot_file> : snapshot file (may be prepared)"
        echo "          delete - delete spare brigades from the datacenter"
        echo "          Options:"
        echo "                  -n : dry run"
        echo "                  -s <datacenter_id> : source datacenter"
        echo "                  -f <snapshot_file> : snapshot file (may be prepared)"
        echo "  Attention! Brigades not really created or deleted, only database records are changed"

        exit 1
}

create () {
        while [ $# -gt 0 ]; do
                case "$1" in
                        -t)
                                DATACENTER_ID="$2"
                                shift 2
                                ;;
                        -f)
                                SNAPSHOT_FILE="$2"
                                shift 2
                                ;;
                        -n)
                                DRY_RUN=yes
                                shift
                                ;;
                        *)
                                printdef "Unknown option: $1"
                                ;;
                esac
        done

        if [ -z "$DATACENTER_ID" ]; then
                echo "Missing datacenter_id"

                printdef
        fi

        if [ -z "$SNAPSHOT_FILE" ]; then
                echo "Missing snapshot_file"

                printdef
        fi

        if [ ! -s "$SNAPSHOT_FILE" ]; then
                echo "Snapshot file not found: $SNAPSHOT_FILE"

                exit 1
        fi

        echo "Create spare brigades in the datacenter $DATACENTER_ID"
        echo "Snapshot file: $SNAPSHOT_FILE"

        # Loop through each item in the JSON array within the "snap" key
        jq -c '.snaps[]' < "$SNAPSHOT_FILE" | while read -r snap; do
                brigade_id_32="$(echo "$snap" | jq -r '.brigade_id')"
                brigade_id="$(echo "${brigade_id_32}=========" | base32 -d 2>/dev/null | hexdump -ve '1/1 "%02x"')"

                echo "Brigade $brigade_id"

                featured_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT 
        realm_id 
FROM 
        :"schema_name".brigadier_realms 
WHERE 
        brigade_id = :'brigade_id' 
        AND featured = true
        AND draft = false;
EOF
)"

                if [ -z "$featured_datacenter" ]; then
                        echo "Brigade $brigade_id is not featured"

                        continue
                fi

                spare_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT
        realm_id
FROM
        :"schema_name".brigadier_realms
WHERE
        brigade_id = :'brigade_id'
        AND featured = false
        AND draft = false;
EOF
)"

                if [ "$spare_datacenter" = "$DATACENTER_ID" ]; then
                        echo "  spare brigade $brigade_id is already in the datacenter $DATACENTER_ID"

                        continue
                fi

                if [ -n "$spare_datacenter" ] && [ "$spare_datacenter" != "$DATACENTER_ID" ]; then
                        echo "  brigade $brigade_id is not in the datacenter $DATACENTER_ID"

                        continue
                fi

                if [ -n "$DRY_RUN" ]; then
                        echo "  brigade $brigade_id is in the datacenter $DATACENTER_ID"
                else
                        psql -d "${DBNAME}" -q -t -A \
                                --set ON_ERROR_STOP=yes \
                                --set schema_name="${SCHEMA}" \
                                --set brigade_id="${brigade_id}" \
                                --set datacenter_id="${DATACENTER_ID}" <<EOF
BEGIN;

INSERT INTO
	:"schema_name".brigadier_realms (brigade_id, realm_id, featured, draft)
VALUES
	(:'brigade_id', :'datacenter_id', false, false);
INSERT INTO
	:"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_name, event_info, event_time)
VALUES
	(:'brigade_id', :'datacenter_id', 'compose', 'spare', NOW() AT TIME ZONE 'UTC');
                
COMMIT;

EOF
                fi
        done

        exit 0
}

switch () {
        while [ $# -gt 0 ]; do
                case "$1" in
                        -t)
                                DATACENTER_ID="$2"
                                shift 2
                                ;;
                        -f)
                                SNAPSHOT_FILE="$2"
                                shift 2
                                ;;
                        -n)
                                DRY_RUN=yes
                                ;;
                        *)
                                printdef "Unknown option: $1"
                                ;;
                esac
        done

        if [ -z "$DATACENTER_ID" ]; then
                echo "Missing datacenter_id"

                printdef
        fi

        if [ -z "$SNAPSHOT_FILE" ]; then
                echo "Missing snapshot_file"

                printdef
        fi

        if [ ! -s "$SNAPSHOT_FILE" ]; then
                echo "Snapshot file not found: $SNAPSHOT_FILE"

                exit 1
        fi

        # Loop through each item in the JSON array within the "snap" key
        jq -c '.snaps[]' < "$SNAPSHOT_FILE" | while read -r snap; do
                brigade_id_32="$(echo "$snap" | jq -r '.brigade_id')"
                brigade_id="$(echo "${brigade_id_32}=========" | base32 -d 2>/dev/null | hexdump -ve '1/1 "%02x"')"

                echo "Brigade $brigade_id"

                featured_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT 
        realm_id 
FROM 
        :"schema_name".brigadier_realms 
WHERE 
        brigade_id = :'brigade_id' 
        AND featured = true
        AND draft = false;
EOF
)"

                if [ -z "$featured_datacenter" ]; then
                        echo "Brigade $brigade_id is not featured"

                        continue
                fi

                if [ "$featured_datacenter" = "$DATACENTER_ID" ]; then
                        echo "Brigade $brigade_id is already in the datacenter $DATACENTER_ID"

                        continue
                fi

                spare_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT
        realm_id
FROM
        :"schema_name".brigadier_realms
WHERE
        brigade_id = :'brigade_id'
        AND featured = false
        AND draft = false;
EOF
)"

                if [ "$spare_datacenter" != "$DATACENTER_ID" ]; then
                        echo "  brigade $brigade_id is not in the datacenter $DATACENTER_ID"

                        continue
                fi

                if [ -n "$DRY_RUN" ]; then
                        echo "Brigade $brigade_id switch to datacenter $DATACENTER_ID"
                else
                        psql -d "${DBNAME}" -q -t -A \
                                --set ON_ERROR_STOP=yes \
                                --set schema_name="${SCHEMA}" \
                                --set brigade_id="${brigade_id}" \
                                --set source_datacenter_id="${featured_datacenter}" \
                                --set target_datacenter_id="${DATACENTER_ID}" <<EOF
BEGIN;

INSERT INTO
	:"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_name, event_info, event_time)
VALUES
	(:'brigade_id', :'target_datacenter_id', 'modify', 'promote', NOW() AT TIME ZONE 'UTC');

INSERT INTO
	:"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_name, event_info, event_time)
VALUES
	(:'brigade_id', :'source_datacenter_id', 'modify', 'retire', NOW() AT TIME ZONE 'UTC');

UPDATE
        :"schema_name".brigadier_realms
SET
        featured = false
WHERE
        brigade_id = :'brigade_id'
        AND realm_id = :'source_datacenter_id'
        AND draft = false;

UPDATE
        :"schema_name".brigadier_realms
SET
        featured = true
WHERE
        brigade_id = :'brigade_id'
        AND realm_id = :'target_datacenter_id'
        AND draft = false;
                
COMMIT;

EOF
                fi

        done

        exit 0
}


delete () {
        while [ $# -gt 0 ]; do
                case "$1" in
                        -s)
                                DATACENTER_ID="$2"
                                shift 2
                                ;;
                        -f)
                                SNAPSHOT_FILE="$2"
                                shift 2
                                ;;
                        -n)
                                DRY_RUN=yes
                                shift 1
                                ;;
                        *)
                                printdef "Unknown option: $1"
                                ;;
                esac
        done

        if [ -z "$DATACENTER_ID" ]; then
                echo "Missing datacenter_id"

                printdef
        fi

        if [ -z "$SNAPSHOT_FILE" ]; then
                echo "Missing snapshot_file"

                printdef
        fi

        if [ ! -s "$SNAPSHOT_FILE" ]; then
                echo "Snapshot file not found: $SNAPSHOT_FILE"

                exit 1
        fi

        echo "DELETE $SNAPSHOT_FILE FROM DATACANTER_ID=$DATACENTER_ID dry_run: $DRY_RUN"

        # Loop through each item in the JSON array within the "snap" key
        jq -c '.snaps[]' < "$SNAPSHOT_FILE" | while read -r snap; do
                brigade_id_32="$(echo "$snap" | jq -r '.brigade_id')"
                brigade_id="$(echo "${brigade_id_32}=========" | base32 -d 2>/dev/null | hexdump -ve '1/1 "%02x"')"

                echo "Brigade $brigade_id"

                featured_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT 
        realm_id 
FROM 
        :"schema_name".brigadier_realms 
WHERE 
        brigade_id = :'brigade_id' 
        AND featured = true
        AND draft = false;
EOF
)"

                if [ -z "$featured_datacenter" ]; then
                        echo "Brigade $brigade_id is not featured"

                        continue
                fi

                if [ "$featured_datacenter" = "$DATACENTER_ID" ]; then
                        echo "Brigade $brigade_id is in old the datacenter $DATACENTER_ID"

                        continue
                fi

                spare_datacenter="$(psql -d "${DBNAME}" -q -t -A \
                        --set ON_ERROR_STOP=yes \
                        --set schema_name="${SCHEMA}" \
                        --set brigade_id="${brigade_id}" <<EOF
SELECT
        realm_id
FROM
        :"schema_name".brigadier_realms
WHERE
        brigade_id = :'brigade_id'
        AND featured = false
        AND draft = false;
EOF
)"

                if [ "$spare_datacenter" != "$DATACENTER_ID" ]; then
                        echo "  spare brigade $brigade_id is not in the datacenter $DATACENTER_ID"

                        continue
                fi

                if [ -n "$DRY_RUN" ]; then
                        echo "Brigade $brigade_id deleted from datacenter $DATACENTER_ID"
                else
                        psql -d "${DBNAME}" -q -t -A \
                                --set ON_ERROR_STOP=yes \
                                --set schema_name="${SCHEMA}" \
                                --set brigade_id="${brigade_id}" \
                                --set datacenter_id="${spare_datacenter}" <<EOF
BEGIN;

INSERT INTO
	:"schema_name".brigadier_realms_actions (brigade_id, realm_id, event_name, event_info, event_time)
VALUES
	(:'brigade_id', :'datacenter_id', 'remove', 'spare', NOW() AT TIME ZONE 'UTC');

DELETE FROM
        :"schema_name".brigadier_realms
WHERE
        brigade_id = :'brigade_id'
        AND realm_id = :'datacenter_id'
        AND featured = false
        AND draft = false;
COMMIT;
EOF
                fi

        done

        exit 0
}

while [ $# -gt 0 ]; do
        cmd="$1"
        shift
        case "$cmd" in
                create)
                        create "$@"
                        ;;
                switch)
                        switch "$@"
                        ;;
                delete)
                        delete "$@"
                        ;;
                -h|--help)
                        printdef
                        ;;
                *)
                        printdef "Unknown command: $1"
                        ;;
        esac
done