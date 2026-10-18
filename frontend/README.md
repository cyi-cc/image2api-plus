# image2api frontend

Vue 3 + Vite frontend for `image2api`: user-facing generation UI (playground,
gallery, docs, billing) plus the admin console (channels, models, users,
orders, logs, config).

## Develop

```bash
npm install
npm run dev        # http://localhost:5173
```

The dev server proxies `/admin`, `/health`, `/images`, `/v1` to the backend.
Start the Go backend separately (see `backend/README.md`).

```bash
# from ../backend
go build -o bin/api ./cmd/api && ./bin/api   # http://127.0.0.1:6666
```
