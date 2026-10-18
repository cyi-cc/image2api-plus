# image2api backend

Go backend for `image2api` — a vivid-protocol gateway that routes generation
jobs across same-protocol upstream channels. Stack: Gin · GORM (PostgreSQL) ·
go-redis.

## Architecture sketch

- `internal/upstream` — vivid-protocol client (NDJSON generate + jobId resume,
  `/v1/uploads`, `/v1/models`, `/v1/balance`, error classification)
- `internal/service/channel.go` — channel CRUD, upstream model sync (save =
  full sync), AES-GCM key storage
- `internal/service/v1.go` — channel-weighted dispatch, billing, job lifecycle,
  stale-job resume
- `internal/http/handler/v1.go` — public vivid endpoints
  (`/v1/generate` `/v1/uploads` `/v1/models` `/v1/balance`)

## Environment

See `.env.example`. Required: `POSTGRES_DSN`, `REDIS_ADDR`, `RUSTFS_*`.
Important: `CHANNEL_SECRET` encrypts stored channel API keys (AES-GCM) — set a
random value and keep it stable; rotating it makes saved keys undecryptable.

```bash
cp .env.example .env   # fill in values
go build -o bin/api ./cmd/api && ./bin/api
```
