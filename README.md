# Mail Log Dashboard API

Simple Go service that ingests `/var/log/maillog` (Postfix-style syslog) into SQLite and exposes a read-only API for a tracking center UI.

## Quick start

```bash
# ingest once
MAILLOG_PATH=/var/log/maillog DB_PATH=./maillog.db go run . -mode=ingest

# ingest only new data since last run (tail)
MAILLOG_PATH=/var/log/maillog DB_PATH=./maillog.db go run . -mode=tail

# follow the log and ingest every 2 minutes (handles rotation/truncation)
MAILLOG_PATH=/var/log/maillog DB_PATH=./maillog.db INGEST_INTERVAL=2m go run . -mode=follow

# run API server
LISTEN_ADDR=:8080 DB_PATH=./maillog.db go run . -mode=serve
```

## API

`GET /api/logs`

Query params:
- `from` / `to`: RFC3339 or `YYYY-MM-DD HH:MM:SS`
- `sender`: substring match on `mail_from`
- `receiver`: substring match on `mail_to`
- `status`: exact match (e.g. `sent`, `bounced`, `deferred`)
- `q`: substring match against raw line
- `limit` / `offset`

Example:

```bash
curl "http://localhost:8080/api/logs?from=2026-01-23T00:00:00Z&to=2026-01-24T00:00:00Z&receiver=domain.co.tz&limit=50"
```

`GET /api/health`

## Notes
- Entries are de-duplicated by a hash of the raw log line.
- Set `AUTO_INGEST=true` and `INGEST_INTERVAL=2m` to auto-ingest when running in serve mode.
- Incremental ingest stores a byte-offset and inode to handle log rotation and truncation.
- Amavis lines populate extra fields: `queuedAs`, `mailId`, `subject`, `hits`, `helo`, `amavisOrigin`.
