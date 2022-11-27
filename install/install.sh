#!/bin/sh

set -e

CONFDIR=${CONFDIR:-"/etc/vgdept"}
echo "confdir: ${CONFDIR}"
DBNAME=${DBNAME:-$(cat ${CONFDIR}/dbname)}
echo "dbname: $DBNAME"
SCHEMA=${SCHEMA:-$(cat ${CONFDIR}/schema)}
echo "schema: $SCHEMA"
REALMS_DBUSER=${REALMS_DBUSER:-$(cat ${CONFDIR}/realms_dbuser)}
echo "realms user: $REALMS_DBUSER"
BRIGADIERS_DBUSER=${BRIGADIERS_DBUSER:-$(cat ${CONFDIR}/brigadiers_dbuser)}
echo "brigadiers user: $BRIGADIERS_DBUSER"

set -x

sudo -i -u postgres psql -v -d ${DBNAME} \
    --set schema_name=${SCHEMA} \
    --set realms_dbuser=${REALMS_DBUSER} \
    --set brigadiers_dbuser=${BRIGADIERS_DBUSER} \
 < `dirname $0`/000-install.sql
