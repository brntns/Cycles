---
name: verify
description: Build, run and drive Varde (Go API + PWA) locally to verify changes end-to-end.
---

# Verifying Varde

## Database (no Docker needed)

Local Postgres binaries work; a scratch cluster is enough:

```bash
initdb -D "$DIR/data" -U test --auth=trust -E UTF8
pg_ctl -D "$DIR/data" -l "$DIR/pg.log" \
  -o "-p 5544 -c listen_addresses=127.0.0.1 -c unix_socket_directories=''" start
createdb -h 127.0.0.1 -p 5544 -U test varde_ui
```

Gotcha: unix sockets fail in deep scratch dirs (107-byte path limit) — disable
them (`unix_socket_directories=''`) and use TCP.

## API tests

```bash
TEST_DATABASE_URL='postgres://test@127.0.0.1:5544/varde_test?sslmode=disable' \
  go test ./internal/httpapi/ -count=1
```

Tests skip without `TEST_DATABASE_URL`. They truncate all tables — never point
them at real data.

## Run the app

```bash
DATABASE_URL='postgres://test@127.0.0.1:5544/varde_ui?sslmode=disable' \
  VARDE_PASSWORD=changeme COOKIE_SECURE=false PORT=4716 go run ./cmd/server
curl -s http://127.0.0.1:4716/health   # {"status":"ok"}
```

Seed via curl: login (`POST /auth/login {"password":"changeme"}` with a cookie
jar), `POST /cycles`, `POST /cycles/{id}/entries`.

Gotcha: login is rate-limited to 5 attempts per IP per 5 minutes, in memory —
repeated scripted logins lock you out; restart the server to reset.

## Drive the UI

Playwright browsers are cached in `~/.cache/ms-playwright/` but the package is
not installed globally: `npm i playwright-core` in a scratch dir, then launch
with `executablePath` pointed at
`~/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell*`.
Use viewport 390×844 (the spec's iPhone target).

Selector gotchas: all screens live in one DOM — scope to `#review-content`,
`#shell-content` etc. or you hit hidden elements (e.g. the login button also
matches `.btn-primary`); on mobile widths the top nav is hidden, click
`.tabbar a[...]` instead.

Flows worth driving: login → status card; "+ Update"; "End cycle…" (complete
walks the state machine forward, bury goes straight to the brain-dump; both
end on the empty state and the cycle appears under History with a terminal
system entry); weekly review via "Start Sunday review".
