# Deployment plan: backend 16 → 30

Status: **PREPARED, NOT EXECUTED.** Requires explicit owner approval.

## What changes

- Backend binary from commit `3db3e15` (or newer) containing routes and services through
  paid messages.
- PostgreSQL migrations 17–30:
  17 simulated mail/reset, 18 engagement, 19 notifications, 20 creator verification,
  21 profile media, 22 message media, 23 scheduled posts, 24 promotions/trials,
  25 privacy/account, 26 safety/DMCA, 27 stories, 28 audio media, 29 welcome message,
  30 paid messages/requests.
- No DNS, firewall, port, TLS, Caddy or provider change. `PAY_PROVIDER=fake`,
  `ENABLE_FAKE_PAYMENTS=true`, simulated email and local media stay as they are.

## Preconditions

1. Owner approval for this exact deployment.
2. Freeze writes or accept a short maintenance window. Existing API can keep serving
   while the backup runs; the window starts at migration.
3. Confirm free disk and record `df -h /`, `pg_database_size`, and current API PID.
4. Build the binary on the development host, not on production:
   `cd backend && CGO_ENABLED=0 go build -trimpath -o bin/api ./cmd/api`
   and `go build -trimpath -o bin/migrate ./cmd/migrate`.
5. Artefact checksums recorded.

## Backup

```
pg_dump -h 127.0.0.1 -p 55433 -U platform -d content_platform -Fc \
  -f /data/downloads/content-platform-runtime/backup-pre30-$(date -u +%Y%m%dT%H%M%SZ).dump
sha256sum <dump>
cp /data/downloads/content-platform-runtime/bin/api <runtime>/bin/api.pre30
```

Keep both until the owner accepts the device evaluation. Do not delete the pre-30 dump.

## Migration

The exact sequence was rehearsed on a restored clone of production; it applied
`[17 18 19 20 21 22 23 24 25 26 27 28 29 30]` with no errors.

```
DATABASE_URL='postgres://…/content_platform?sslmode=disable' ./bin/migrate
psql … -c "select max(version), count(*) from public.schema_migrations"
```

Expected: max 30, count 30. Migrations are additive. If a migration fails, stop and do
not start the new binary; keep the old binary running.

## Binary swap

```
kill -TERM "$(pgrep -f '/content-platform-runtime/bin/api')"
# wait for exit and the 20s shutdown timeout
install -m 0755 bin/api /data/downloads/content-platform-runtime/bin/api.new
mv /data/downloads/content-platform-runtime/bin/api.new \
   /data/downloads/content-platform-runtime/bin/api
# start with the same environment used by the existing process (api.env), same port
nohup /data/downloads/content-platform-runtime/bin/api >> runtime/log/api.log 2>&1 &
```

Do not change Caddy. Do not expose a new port.

## Verification (read-only first)

```
curl -fsS https://…/readyz
curl -fsS https://…/v1/meta
curl -sS -o /dev/null -w '%{http_code}\n' https://…/v1/notifications
curl -sS -o /dev/null -w '%{http_code}\n' https://…/v1/dm/settings
```

Expected: readyz `database: ok`, meta still `provider: fake, simulated: true`,
notifications and settings return 401 (route exists, auth required), not 404.

Then sign in on the device and run the test plan. No production data writes beyond the
normal test account usage without separate approval.

## Rollback

- The pre-30 binary is retained. Migrations 17–30 are additive, so the old binary can
  still run against the migrated schema if rollback is urgent.
- If schema rollback is ever required, restore the pre-30 dump into a new database and
  switch the connection string; do not run destructive down-SQL on production.
- Record the rollback decision and time in the operations log.
