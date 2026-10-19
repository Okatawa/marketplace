#!/bin/sh
set -eu
for service in accounts listings search messaging notifications; do
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres --set app_password="$APP_DB_PASSWORD" --set service="$service" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'service', :'app_password') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'service', :'service') \gexec
SQL
done
