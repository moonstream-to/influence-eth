#!/usr/bin/env sh

# Compile application and run with provided arguments
set -e

PROGRAM_NAME="influence-eth"

go build -o "$PROGRAM_NAME" .

./"$PROGRAM_NAME" "$@"
