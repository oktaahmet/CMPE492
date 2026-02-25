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

Note : Private key of the wallet used to send USDC test coins to workers, you can get some test USDC here(select base sepolia network):  https://faucet.circle.com/


## 3. Start Services

Build and start all services with Docker:
```bash
docker compose up --build
```
## 4. Open the web page
http://localhost:5173/

## API documentation
See at: http://localhost:8080/api/docs/
