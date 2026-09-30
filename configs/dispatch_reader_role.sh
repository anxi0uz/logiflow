#!/bin/sh
set -eu

psql -X -v ON_ERROR_STOP=1 \
  -v dispatch_password="$DISPATCH_DATABASE_PASSWORD" \
  -f /bootstrap/dispatch_reader_role.sql
