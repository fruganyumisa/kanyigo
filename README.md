# Mail Log Dashboard

A containerized mail log dashboard with a Go API, PostgreSQL storage, and a Next.js frontend. It ingests `/var/log/maillog` Postfix-style syslog entries and exposes searchable delivery records for an operations UI.

## Quick start

```bash
# development defaults
docker compose up --build
```

The frontend runs on `http://localhost:3000` and the API runs on `http://localhost:8080`.

Development admin login:
- Email: `admin@example.com`
- Password: `admin12345`

These defaults are rejected when `APP_ENV=production`.

## Production setup

Create a `.env` from `.env.example` and replace every password/origin value before first startup:

```bash
cp .env.example .env
# edit .env
docker compose --env-file .env up --build -d
```

Production requirements:
- Set `APP_ENV=production`.
- Set a strong `POSTGRES_PASSWORD`.
- Set `ADMIN_EMAIL` and an `ADMIN_PASSWORD` of at least 12 characters.
- Set `ALLOWED_ORIGINS` to the public frontend origin, for example `https://logs.example.com`.
- Put the service behind TLS termination such as Nginx, Caddy, Traefik, or a load balancer.

The Compose file publishes only the frontend publicly by default. PostgreSQL and the API bind to `127.0.0.1` on the host for local administration and should not be exposed directly to the internet.

By default Compose mounts `/var/log/maillog` into the API container. To use a different file:

```bash
MAILLOG_PATH=/path/to/maillog docker compose up --build
```

## Backend commands

Run these against a reachable PostgreSQL database:

```bash
export DATABASE_URL='postgres://logs:logs@localhost:5432/logs_dashboard?sslmode=disable'

# ingest once
MAILLOG_PATH=/var/log/maillog go run . -mode=ingest

# ingest only new data since last run (tail)
MAILLOG_PATH=/var/log/maillog go run . -mode=tail

# follow the log and ingest every 2 minutes (handles rotation/truncation)
MAILLOG_PATH=/var/log/maillog INGEST_INTERVAL=2m go run . -mode=follow

# run API server
LISTEN_ADDR=:8080 go run . -mode=serve
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
- `/api/logs` is protected by an HTTP-only session cookie.
- Admin users can create other dashboard users from the frontend.
- Expired sessions are cleaned up opportunistically during login.
- API CORS allows only origins listed in `ALLOWED_ORIGINS`.
- API pagination is capped at `500` records per request.
- Set `AUTO_INGEST=true` and `INGEST_INTERVAL=2m` to auto-ingest when running in serve mode.
- Incremental ingest stores a byte-offset and inode to handle log rotation and truncation.
- Amavis lines populate extra fields: `queuedAs`, `mailId`, `subject`, `hits`, `helo`, `amavisOrigin`.
- The frontend proxies browser requests to the API with `API_INTERNAL_BASE`, which defaults to `http://localhost:8080` for local development.
