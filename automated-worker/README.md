# Automated Worker

This folder contains the Playwright-based browser worker launcher. It is separate from `frontend/` on purpose.


- launches a real Chromium browser with Playwright
- opens many pages against your frontend
- each page loads `?auto_worker=1#/`
- each page auto-generates a random `0x...` worker id and starts working

## Install

```bash
cd automated-worker
npm install
npm run playwright:install
```

## Run

```bash
npm run workers:browser -- --count 30
```

Useful examples:

```bash
npm run workers:browser -- --count 50 --headless
npm run workers:browser -- --count 20 --base-url http://127.0.0.1:4173/
npm run workers:browser -- --count 10 --stagger-ms 500
```

By default the script targets:

```text
http://127.0.0.1:5173/?auto_worker=1#/
```
