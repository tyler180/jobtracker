#!/usr/bin/env bash
# Rehearse against a disposable, loopback-only PostgreSQL 18 database.
set -euo pipefail

if [[ $# != 3 ]]; then
  echo "Usage: $0 /absolute/archive.tar.gz expected-sha256 /absolute/report-directory" >&2
  exit 2
fi
archive_path=$1
archive_sha=$2
report_dir=$3
if [[ "$archive_path" != /* || "$report_dir" != /* || -e "$report_dir" ]]; then
  echo "Use absolute paths and a new report directory." >&2
  exit 2
fi
cd "$(dirname "$0")/.."
umask 077
mkdir -p "$report_dir"
export GOCACHE="${GOCACHE:-/tmp/jobtracker-go-cache}"
go run ./cmd/jobtracker-migrate -archive "$archive_path" -sha256 "$archive_sha" > "$report_dir/backup-validation.json"

test_container="jobtracker-migration-test-$$"
cleanup() { docker rm -fv "$test_container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$test_container" \
  --label jobtracker.purpose=migration-test \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  -e POSTGRES_DB=jobtracker_migration_test \
  -p 127.0.0.1::5432 \
  postgres:18@sha256:b9f6b8ff03b62f3cfd9d9ba3fe56226a9991c79d3fb46fd46536263bf02be553 >/dev/null
ready=false
for (( attempt=0; attempt<60; attempt++ )); do
  if docker exec "$test_container" pg_isready -U postgres -d jobtracker_migration_test >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  echo "Temporary PostgreSQL did not become ready." >&2
  exit 1
fi
test_port=$(docker port "$test_container" 5432/tcp)
test_port=${test_port##*:}
export JOBTRACKER_MIGRATION_TEST_URL="postgres://postgres@127.0.0.1:${test_port}/jobtracker_migration_test?sslmode=disable"
export JOBTRACKER_MIGRATION_BACKUP="$archive_path"
export JOBTRACKER_MIGRATION_BACKUP_SHA256="$archive_sha"
go test -count=1 -race -v ./... > "$report_dir/tests.log" 2>&1
go vet ./... > "$report_dir/vet.log" 2>&1
go build -o "$report_dir/jobtracker-migrate" ./cmd/jobtracker-migrate
docker exec "$test_container" psql -U postgres -d jobtracker_migration_test -Atc 'SELECT version()' > "$report_dir/postgresql-version.txt"
export MIGRATION_DATABASE_URL="$JOBTRACKER_MIGRATION_TEST_URL"
"$report_dir/jobtracker-migrate" -init-schema -apply > "$report_dir/schema-init.json"
"$report_dir/jobtracker-migrate" -init-schema -apply > "$report_dir/schema-init-repeat.json"
if "$report_dir/jobtracker-migrate" -init-schema > /dev/null 2>&1; then
  echo "Schema initialization unexpectedly succeeded without -apply." >&2
  exit 1
fi
"$report_dir/jobtracker-migrate" -archive "$archive_path" -sha256 "$archive_sha" \
  -issuer https://identity.example.test/application/o/jobtracker/ -subject rehearsal-owner \
  -apply -apply-schema > "$report_dir/import-first.json"
"$report_dir/jobtracker-migrate" -archive "$archive_path" -sha256 "$archive_sha" \
  -issuer https://identity.example.test/application/o/jobtracker/ -subject rehearsal-owner \
  -apply > "$report_dir/import-repeat.json"
rm "$report_dir/jobtracker-migrate"
python3 - "$report_dir" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
first = json.loads((p / 'import-first.json').read_text())
repeat = json.loads((p / 'import-repeat.json').read_text())
assert json.loads((p / 'schema-init.json').read_text())['schema_version'] == 2
assert json.loads((p / 'schema-init-repeat.json').read_text())['schema_version'] == 2
assert first['inserted'] == first['verified'] == first['records']
assert repeat['inserted'] == 0
assert repeat['unchanged'] == repeat['verified'] == first['records']
report = {
    'archive_sha256': first['archive_sha256'],
    'records_verified': first['verified'],
    'first_import_inserted': first['inserted'],
    'repeat_import_inserted': repeat['inserted'],
    'raw_json_and_all_fields_preserved': True,
    'owner_scoped_data_access_tests_passed': True,
    'conflict_rollback_and_stale_backup_tests_passed': True,
    'full_race_test_suite_passed': True,
    'go_vet_passed': True,
    'cli_build_and_execution_passed': True,
    'schema_initialization_and_repeat_passed': True,
    'signed_oidc_and_authenticated_http_tests_passed': True,
    'test_identity_only': True,
    'live_app_changed': False,
    'live_database_changed': False,
    'authentik_changed': False,
}
(p / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
PY
cleanup
trap - EXIT
echo "Temporary PostgreSQL container and its data volume removed. Reports: $report_dir"
