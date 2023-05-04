BEGIN;

CREATE TABLE :"schema_name".brigades_actions (
    brigade_id          uuid NOT NULL REFERENCES :"schema_name".brigadiers_ids (brigade_id),
    event_time          timestamp without time zone NOT NULL,
    event_name          text NOT NULL,
    event_info          text NOT NULL,
    update_time         timestamp without time zone NOT NULL DEFAULT NOW(),
    PRIMARY KEY (brigade_id, event_time)
);

COMMIT;