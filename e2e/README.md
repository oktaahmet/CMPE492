# E2E Tests

This package contains Playwright system tests. It is separate from
`automated-worker/`, which is only a helper for manually launching many browser
workers.

## Install

```bash
cd e2e
npm install
npm run playwright:install
```

## Run

Start the Docker stack with the E2E override. It leaves `.env` untouched while
enabling local auto-workers and booting the short bundled workflow:

```bash
docker compose -f docker-compose.yml -f docker-compose.e2e.yml up --build
```

Then run:

```bash
cd e2e
npm run workflow
```

The workflow test opens real Chromium auto-worker pages, resets and completes
the bundled mini workflow, checks `/api/runtime`, and verifies persisted payment
events through `/api/payments`.

Useful variants:

```bash
npm run smoke -- --headed
npm run smoke -- --skip-auto-worker
npm run workflow -- --worker-count 6
```

The workflow command requires `ADMIN_API_TOKEN` or `E2E_ADMIN_TOKEN` to match
the backend admin token. The script reads `ADMIN_API_TOKEN` from the repo `.env`
when it is not already exported in the shell.
