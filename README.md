# Youth Training Load Ledger

Youth Training Load Ledger is a Go backend for governing summer training records for minors. It keeps guardian consent, health screening, baseline assessments, versioned weekly prescriptions, activity load, fatigue risk, pauses, resumptions, reassessments and professional audit evidence in one chronological record.

## Runtime

- Go 1.24
- SQLite with WAL mode, foreign keys and three versioned migrations
- HTTP JSON API on `:8080`
- Persistent retrying worker for risk notifications
- Server-side revocable bearer sessions

Start locally:

```sh
cp .env.example .env
go run ./cmd/server
```

No default credentials are created. To initialize the first health advisor, set both `BOOTSTRAP_ADMIN` and `BOOTSTRAP_PASSWORD` for one startup; providing only one makes startup fail. Session tokens are returned only at login; the database stores SHA-256 token digests. Passwords use PBKDF2-SHA256 with per-user random salt.

## API overview

- `GET /healthz`, `GET /readyz`
- `POST /v1/auth/login`, `POST /v1/auth/logout`, `GET /v1/me`
- athlete profile, guardian consent, screening, baseline and activation operations
- versioned prescription creation/publication and strength blocks
- idempotent activity recording with recovery and rolling-load checks
- fatigue reports, risk acknowledgement/resolution, reassessment and resume
- paginated athlete and audit queries with relationship-based authorization

Every error response contains a stable code, readable message and request ID. Mutating professional actions preserve their basis in `audit_events`; profile status changes also append `status_transitions`.

## Verification

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

Container builds use the real `./cmd/server` entrypoint:

```sh
docker build --platform linux/amd64 -t youth-training-load-ledger:amd64 .
docker build --platform linux/arm64 -t youth-training-load-ledger:arm64 .
```

The application creates the database from an empty file, checks migration identity on repeated starts, and resumes pending or expired-lease worker jobs after restart.
