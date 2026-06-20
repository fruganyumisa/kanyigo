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

# continuously follow the log into PostgreSQL (handles rotation/truncation)
MAILLOG_PATH=/var/log/maillog go run . -mode=follow

# run API server
LISTEN_ADDR=:8080 go run . -mode=serve
```

## Mail log permissions

Run the backend as a dedicated non-root account such as `logs-dashboard`. Do not
run the entire process as `root` just to read `/var/log/maillog`.

Grant the service account read access with a POSIX ACL:

```bash
sudo setfacl -m u:logs-dashboard:r /var/log/maillog
```

For the provided distroless container, grant the image's dedicated non-root UID:

```bash
sudo setfacl -m u:65532:r /var/log/maillog
```

Because log rotation creates a new file, configure the mail log's `logrotate`
rule to apply the ACL after rotation, or grant access through the distribution's
log-reading group:

```bash
sudo usermod -aG adm logs-dashboard
# Some distributions use a "log" group instead of "adm".
```

## Nginx brute-force monitoring

The API continuously reads the Nginx access log when
`AUTO_NGINX_SECURITY_INGEST=true`. It flags an IP after 10 consecutive `404`
responses within two minutes. A `200`, redirect, or any other non-404 response
resets that IP's streak. Ten `401`/`403` responses within five minutes are also
flagged. Thresholds, windows, ignored paths, trusted proxies, and protected IPs
are configurable through the `BRUTEFORCE_*`, `TRUSTED_PROXY_CIDRS`, and
`SECURITY_IP_ALLOWLIST` variables in `docker-compose.yml`.

JSON access logs are recommended:

```nginx
log_format dashboard_json escape=json '{'
  '"time_iso8601":"$time_iso8601",'
  '"remote_addr":"$remote_addr",'
  '"http_x_forwarded_for":"$http_x_forwarded_for",'
  '"request_method":"$request_method",'
  '"uri":"$request_uri",'
  '"status":$status,'
  '"http_user_agent":"$http_user_agent"'
'}';
access_log /var/log/nginx/access.log dashboard_json;
```

The parser also accepts the standard Nginx combined format. Grant the API's
non-root account read access to this log using the same ACL/group approach used
for `/var/log/maillog`.

## Firewall agent

The API never receives root or `CAP_NET_ADMIN`. Blocking is performed by a
small host agent over a protected Unix socket:

```bash
sudo groupadd --system logs-dashboard 2>/dev/null || true
go build -o /tmp/logs-dashboard-firewall-agent ./cmd/firewall-agent
sudo install -o root -g root -m 0755 /tmp/logs-dashboard-firewall-agent /usr/local/sbin/logs-dashboard-firewall-agent
sudo install -o root -g root -m 0644 deploy/logs-dashboard-firewall-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now logs-dashboard-firewall-agent
```

Install the host packages that provide `ipset`, `iptables`, and `ip6tables`.
For a host-installed Nginx, keep `FIREWALL_CHAIN=INPUT`. If Nginx is published
through Docker, use `FIREWALL_CHAIN=DOCKER-USER` and order the agent after
`docker.service` with a systemd override. Set `HOST_ACCESS_GID` in Compose to
the numeric GID of the `logs-dashboard` host group so the non-root API can use
the socket:

```bash
getent group logs-dashboard
```

Set the agent's `FIREWALL_IP_ALLOWLIST` to the same protected networks as the
API's `SECURITY_IP_ALLOWLIST`. The agent enforces its own allowlist even if a
different local process gains access to the Unix socket.

Only one API replica should ingest Nginx logs. Additional replicas should set
`AUTO_NGINX_SECURITY_INGEST=false`.

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
- Raw entries are de-duplicated by a hash of the source line and retained for audit.
- `/api/logs` returns stitched Queue ID transactions rather than fragmented source lines.
- `/api/logs` is protected by an HTTP-only session cookie.
- Admin users can create other dashboard users from the frontend.
- Expired sessions are cleaned up opportunistically during login.
- API CORS allows only origins listed in `ALLOWED_ORIGINS`.
- API pagination is capped at `500` records per request.
- Set `AUTO_INGEST=true` to run continuous ingestion inside the API service.
- Run `AUTO_INGEST=true` on only one API replica; additional API replicas should set it to `false`.
- Continuous ingest drains rotated descriptors and stores the processed byte-offset and inode transactionally in PostgreSQL.
- In-flight Queue ID transactions are persisted in PostgreSQL so stitching resumes correctly after a restart.
- Parser workers process raw lines concurrently while checkpoint commits remain in source-file order.
- Completed and timed-out stitched transactions are JSON-encoded to stdout for downstream log shipping.
- Ingest tuning variables: `MAILLOG_POLL_INTERVAL` (default `250ms`), `MAILLOG_ROTATION_DRAIN_TIMEOUT` (default `1s`), `MAILLOG_QUEUE_IDLE_TIMEOUT` (default `30m`), and `MAILLOG_PROCESSING_WORKERS` (default: at least `2`, based on available CPUs).
- Amavis and Dovecot LMTP lines populate junk classification fields: `isJunk` and `spamScore`.
- The frontend proxies browser requests to the API with `API_INTERNAL_BASE`, which defaults to `http://localhost:8080` for local development.
