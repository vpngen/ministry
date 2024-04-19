#!/bin/sh

set -e

if [ -x "../recodesnaps" ]; then
        RECODE="$(dirname "$0")/../recodesnaps"
elif go version >/dev/null 2>&1; then
        RECODE="go run $(dirname "$0")/../"
else
        echo "No snap tool found"
        exit 1
fi

FORCE=${FORCE:-""}

while [ $# -gt 0 ]; do
        case "$1" in
        -force)
                FORCE="$1"
                ;;
        -fp)
                REALM_FP="$2"
                ;;
        *)
                echo "Unknown option: $1"
                exit 1
                ;;
        esac
        shift
done

DB_DIR=${DB_DIR:-"../../../../vpngen-keydesk/cmd/keydesk"}
DB_DIR="$(realpath "${DB_DIR}")"
CONF_DIR=${CONF_DIR:-"../../../../vpngen-keydesk-snap/core/crypto/testdata"}
CONF_DIR="$(realpath "${CONF_DIR}")"

REENCODED_SNAPSHOT_FILE=${REENCODED_SNAPSHOT_FILE:-"${DB_DIR}/brigade.snapshot.reencrypt.json"}

if [ ! -s "${REENCODED_SNAPSHOT_FILE}" ]; then
        echo "No reencoded snapshot found ${REENCODED_SNAPSHOT_FILE}"
        exit 1
fi

AUTHORITY_PRIV_KEY_FILE=${AUTHORITY_PRIV_KEY_FILE:-"${CONF_DIR}/id_rsa_auth1-sample"}

if [ ! -s "${AUTHORITY_PRIV_KEY_FILE}" ]; then
        echo "No authority private key found in ${AUTHORITY_PRIV_KEY_FILE}"
        exit 1
fi

MASTER_PRIV_KEY_FILE=${MASTER_PRIV_KEY_FILE:-"${DB_DIR}/vg-shuffler.priv"}

if [ ! -s "${MASTER_PRIV_KEY_FILE}" ]; then
        echo "No master private key found in ${MASTER_PRIV_KEY_FILE}"
        exit 1
fi

REALMS_FILE=${REALMS_FILE:-"${CONF_DIR}/realms_keys"}

if [ ! -s "${REALMS_FILE}" ]; then
        echo "No realms keys found in ${REALMS_FILE}"
        exit 1
fi

REALM_FP=${REALM_FP:-"SHA256:$(grep "ssh-rsa" "${REALMS_FILE}" | head -n 2 | tail -n 1 | awk '{print $2}' | base64 -d | openssl dgst -sha256 -binary | base64 -w 0 | sed 's/=//g' | awk '{print $1}' )"}

echo "Testing snapshot recoding"
echo "Using authority private key: ${AUTHORITY_PRIV_KEY_FILE}"
echo "Using master private key: ${MASTER_PRIV_KEY_FILE}"
echo "Using target realm fingerprint: ${REALM_FP}"
echo "Using realms file: ${REALMS_FILE}"

${RECODE} \
        -tfp "${REALM_FP}" \
        -rkeys "${REALMS_FILE}" \
        -akey "${AUTHORITY_PRIV_KEY_FILE}" \
        -mkey "${MASTER_PRIV_KEY_FILE}" \
        -in "${REENCODED_SNAPSHOT_FILE}" \
        -out "${DB_DIR}/migration-plan.json" \
        -c "${DB_DIR}/reservation.json" \
        "${FORCE}"
