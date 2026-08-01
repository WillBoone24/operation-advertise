# Operation Advertise — Backend (Phase 1)

Go backend for the portfolio site. SQLite storage, JWT auth, bcrypt password hashing.

## Run it

```bash
export JWT_SECRET="$(openssl rand -base64 48)"   # must be >= 32 bytes
export PORT=8080                                  # optional, defaults to 8080
export DB_PATH=storage/users.db                    # optional
export ALLOWED_ORIGINS="http://localhost:5173"      # optional, comma-separated

go run ./cmd/server
```

## Endpoints

| Method | Path              | Auth | Description                     |
|--------|-------------------|------|----------------------------------|
| GET    | /health           | no   | liveness check                   |
| POST   | /api/register     | no   | create account, returns JWT      |
| POST   | /api/login        | no   | authenticate, returns JWT        |
| GET    | /api/me           | yes  | current user's public profile    |
| POST   | /api/easteregg    | yes  | mark easter egg found (idempotent)|

Send the JWT as `Authorization: Bearer <token>`.

## Build / test

```bash
go build ./...
go vet ./...
```

## Notes on this fix pass

The repo as handed off referenced `gorilla/mux` and `gorilla/handlers` in
`cmd/server/main.go` but neither was declared in `go.mod`/`go.sum`, so the
module would not build. This pass:

- Added `github.com/gorilla/mux v1.8.1` and `github.com/gorilla/handlers v1.5.2`
  (plus their indirect dependency `github.com/felixge/httpsnoop`) to `go.mod`.
- Added a `replace golang.org/x/crypto => github.com/golang/crypto v0.24.0`
  directive. This is a workaround specific to this sandbox's network
  egress allowlist, which permits `github.com` but not `proxy.golang.org`
  or `golang.org`. **If you build this on a normal machine with unrestricted
  network access, delete that replace line** — `golang.org/x/crypto` will
  resolve fine on its own via the standard module proxy.
- Regenerated `go.sum` accordingly.
- Verified the full request lifecycle end-to-end (see below) — build,
  vet, and a live run of every endpoint all pass.

## Verified locally

- `go build ./...` — clean
- `go vet ./...` — clean
- Live smoke test: register → duplicate register (409) → login →
  wrong-password login (401) → unauthenticated `/api/me` (401) →
  authenticated `/api/me` → `/api/easteregg` (first_discovery: true) →
  `/api/easteregg` again (first_discovery: false, idempotent) → `/api/me`
  reflects `easter_egg_found: true` → CORS preflight correctly omits
  `Access-Control-Allow-Origin` for a disallowed origin and includes it
  for an allowed dev origin.
