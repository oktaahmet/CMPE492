package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"x402-scheduler/internal/scheduler"

	"github.com/joho/godotenv"
)

func loadPaymentProvider() scheduler.PaymentProvider {
	baseURL := strings.TrimSpace(os.Getenv("X402_FACILITATOR_URL"))
	apiKeyID := strings.TrimSpace(os.Getenv("CDP_API_KEY_ID"))
	apiPrivateKey := strings.TrimSpace(os.Getenv("CDP_PRIVATE_KEY"))
	payerPrivateKey := strings.TrimSpace(os.Getenv("X402_PAYER_PRIVATE_KEY"))

	network := strings.TrimSpace(os.Getenv("X402_NETWORK"))

	amountDecimals := 6
	if raw := strings.TrimSpace(os.Getenv("X402_AMOUNT_DECIMALS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			amountDecimals = parsed
		}
	}

	authValidity := 10 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("X402_AUTH_VALIDITY_SECONDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			authValidity = time.Duration(parsed) * time.Second
		}
	}

	provider, err := scheduler.NewX402PaymentProvider(scheduler.X402ProviderConfig{
		BaseURL:         baseURL,
		CDPAPIKeyID:     apiKeyID,
		CDPPrivateKey:   apiPrivateKey,
		Network:         network,
		AssetAddress:    strings.TrimSpace(os.Getenv("X402_ASSET_ADDRESS")),
		PayerPrivateKey: payerPrivateKey,
		TokenName:       strings.TrimSpace(os.Getenv("X402_TOKEN_NAME")),
		TokenVersion:    strings.TrimSpace(os.Getenv("X402_TOKEN_VERSION")),
		AmountDecimals:  amountDecimals,
		AuthValidity:    authValidity,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to initialize x402 payment provider: %v", err)
	}
	return provider
}

func loadReplicationFactor() int {
	raw := strings.TrimSpace(os.Getenv("REPLICATION_FACTOR"))
	if raw == "" {
		log.Println("replication factor: 1 (default)")
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		log.Printf("invalid REPLICATION_FACTOR=%q, using 1", raw)
		return 1
	}
	log.Printf("replication factor: %d", n)
	return n
}

func loadEnvFile() {
	var path string = filepath.Join("..", "..", ".env")
	if _, err := os.Stat(path); err != nil {
		log.Fatalf("failed to load .env file: %v", err)
	}
	if err := godotenv.Load(path); err != nil {
		log.Fatalf("failed to load .env file: %v", err)
	}
	log.Printf(".env file loaded from %s", path)
}
