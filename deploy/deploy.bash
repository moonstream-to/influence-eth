#!/usr/bin/env bash

# Deployment script

# Colors
C_RESET='\033[0m'
C_RED='\033[1;31m'
C_GREEN='\033[1;32m'
C_YELLOW='\033[1;33m'

# Logs
PREFIX_INFO="${C_GREEN}[INFO]${C_RESET} [$(date +%d-%m\ %T)]"
PREFIX_WARN="${C_YELLOW}[WARN]${C_RESET} [$(date +%d-%m\ %T)]"
PREFIX_CRIT="${C_RED}[CRIT]${C_RESET} [$(date +%d-%m\ %T)]"

# Main
AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-west-1}"
APP_DIR="${APP_DIR:-/home/ubuntu/influence-eth}"
SECRETS_DIR="${SECRETS_DIR:-/home/ubuntu/influence-eth-secrets}"
PARAMETERS_ENV_PATH="${SECRETS_DIR}/app.env"
FROM_BLOCK_FILE_PATH="${SECRETS_DIR}/from-block.txt"
SCRIPT_DIR="$(realpath $(dirname $0))"
USER_SYSTEMD_DIR="${USER_SYSTEMD_DIR:-/home/ubuntu/.config/systemd/user}"

# Service files
EVENTS_SERVICE_FILE="influence-eth-events.service"
EVENTS_TIMER_FILE="influence-eth-events.timer"
LEADERBOARDS_SERVICE_FILE="influence-eth-leaderboards.service"
LEADERBOARDS_TIMER_FILE="influence-eth-leaderboards.timer"

set -eu

echo
echo
echo -e "${PREFIX_INFO} Building executable script with Go"
EXEC_DIR=$(pwd)
cd "${APP_DIR}"
HOME=/home/ubuntu /usr/local/go/bin/go build -o "${APP_DIR}/influence-eth" .
cd "${EXEC_DIR}"

echo
echo
echo -e "${PREFIX_INFO} If file from-block.txt does not exists, create new one and push deployment block of contract"
if [ ! -f "${FROM_BLOCK_FILE_PATH}" ]; then
  touch "${FROM_BLOCK_FILE_PATH}"
  echo -e "${PREFIX_WARN} Created new from-block file at ${FROM_BLOCK_FILE_PATH}"
  
  source "${APP_DIR}/starknet.sepolia.env"
  HOME=/home/ubuntu "${APP_DIR}/influence-eth" find-deployment-block --contract "${INFLUENCE_DISPATCHER_ADDRESS}" > "${FROM_BLOCK_FILE_PATH}"
fi

echo
echo
echo -e "${PREFIX_INFO} Prepare user systemd directory"
if [ ! -d "${USER_SYSTEMD_DIR}" ]; then
  mkdir -p "${USER_SYSTEMD_DIR}"
  echo -e "${PREFIX_WARN} Created new user systemd directory"
fi

echo
echo
echo -e "${PREFIX_INFO} Replacing existing influence-eth-events service and timer with ${EVENTS_SERVICE_FILE}, ${EVENTS_TIMER_FILE}"
chmod 644 "${SCRIPT_DIR}/${EVENTS_SERVICE_FILE}" "${SCRIPT_DIR}/${EVENTS_TIMER_FILE}"
cp "${SCRIPT_DIR}/${EVENTS_SERVICE_FILE}" "${USER_SYSTEMD_DIR}/${EVENTS_SERVICE_FILE}"
cp "${SCRIPT_DIR}/${EVENTS_TIMER_FILE}" "${USER_SYSTEMD_DIR}/${EVENTS_TIMER_FILE}"
XDG_RUNTIME_DIR="/run/user/$UID" systemctl --user daemon-reload
XDG_RUNTIME_DIR="/run/user/$UID" systemctl --user restart --no-block "${EVENTS_TIMER_FILE}"

echo
echo
echo -e "${PREFIX_INFO} Replacing existing influence-eth-events service and timer with ${LEADERBOARDS_SERVICE_FILE}, ${LEADERBOARDS_TIMER_FILE}"
chmod 644 "${SCRIPT_DIR}/${LEADERBOARDS_SERVICE_FILE}" "${SCRIPT_DIR}/${LEADERBOARDS_TIMER_FILE}"
cp "${SCRIPT_DIR}/${LEADERBOARDS_SERVICE_FILE}" "${USER_SYSTEMD_DIR}/${LEADERBOARDS_SERVICE_FILE}"
cp "${SCRIPT_DIR}/${LEADERBOARDS_TIMER_FILE}" "${USER_SYSTEMD_DIR}/${LEADERBOARDS_TIMER_FILE}"
XDG_RUNTIME_DIR="/run/user/$UID" systemctl --user daemon-reload
XDG_RUNTIME_DIR="/run/user/$UID" systemctl --user restart --no-block "${LEADERBOARDS_TIMER_FILE}"
