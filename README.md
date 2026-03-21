# CMPE492
X402 Payment Based WebAssembly Job Workflow Scheduler for Browsers

# Docker Quick Start


## 1. Create `.env` File

Create a `.env` file in the project root directory.

You can copy it from the `.env-example` file

## 2. Generate Coinbase CDP API Keys

Go to: https://portal.cdp.coinbase.com/

Create a new API Key and Secret Key

Open your .env file and fill in the following values:

CDP_API_KEY_ID=your_api_key_here  
CDP_PRIVATE_KEY=your_secret_key_here  
X402_PAYER_PRIVATE_KEY=your_wallet_private_key_here

WORKER_JWT_SECRET=put_a-long-random-secret-here

ADMIN_API_TOKEN=put-a-random-secret-token-here (this will be the password in the admin page)

Note : Private key of the wallet used to send USDC test coins to workers, you can get some test USDC here(select Base Sepolia network):  https://faucet.circle.com/


## 3. Start Services

Build and start all services with Docker:
```bash
docker compose up --build
```
## 4. Open the web page
http://localhost:5173/


## Local Multi-Worker Test Mode

If you only want to test many concurrent browser workers and wallet verification is not important for that run, use local test mode:

1. Set `WORKER_AUTH_DISABLED=1` in `.env`.
2. Start the stack with `docker compose up`.
3. Run the Playwright helper under `automated-worker/`.

In this mode:

- backend skips wallet/JWT verification for worker endpoints
- `?auto_worker=1` pages generate a random wallet-like `0x...` worker id
- each opened page starts working automatically

Important Note: This mode is intended only for local load/concurrency testing. Keep `WORKER_AUTH_DISABLED=0` for normal demos and real wallet-authenticated runs.


## API documentation
See at: http://localhost:8080/api/docs/
