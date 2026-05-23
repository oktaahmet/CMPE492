# CMPE492
X402 Payment Based WebAssembly Job Workflow Scheduler for Browsers

## Quick start (development)

1. Copy the env template and fill in the required secrets:

   ```bash
   cp .env-example .env
   ```

   In `.env` set at minimum:
   - `CDP_API_KEY_ID`, `CDP_PRIVATE_KEY` — from https://portal.cdp.coinbase.com/
   - `X402_PAYER_PRIVATE_KEY` — hex private key of a wallet funded with
     Base Sepolia USDC (get test funds at https://faucet.circle.com/)
   - `WORKER_JWT_SECRET` — any long random string
   - `ADMIN_API_TOKEN` — any long random string (this is also the admin
     UI password)

2. Build and start the stack:

   ```bash
   docker compose up --build
   ```

3. Open the worker UI:

   - Worker: http://localhost:5173/
   - Admin panel: http://localhost:5173/admin/
   - Live workflow: http://localhost:5173/runtime
   - Swagger API: http://localhost:8080/api/docs/

## Documentation

| Page                                                                                  | For                                                                  |
| ------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| [User Manual](https://github.com/oktaahmet/CMPE492/wiki/User-Manual)                  | Workers (open the site, connect a wallet, earn USDC)                 |
| [System Manual](https://github.com/oktaahmet/CMPE492/wiki/System-Manual)              | Operators (architecture, configuration, API, admin panel) |
| [Workflow Authoring Guide](https://github.com/oktaahmet/CMPE492/wiki/Workflow-Authoring-Guide)                          | Workflow creation, protocols, helpers, rules, examples            |
| [Deployment](https://github.com/oktaahmet/CMPE492/wiki/Deployment)                    | Production deploy (Caddy / HTTPS, prod compose) |
| [Testing](https://github.com/oktaahmet/CMPE492/wiki/Testing)                          | Running the tests (unit, E2E, load, benchmark)             |

