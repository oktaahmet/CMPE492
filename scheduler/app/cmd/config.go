package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"x402-scheduler/internal/scheduler"
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

func loadPostgresDSN() string {
	if raw := strings.TrimSpace(os.Getenv("DATABASE_URL")); raw != "" {
		return raw
	}

	host := strings.TrimSpace(os.Getenv("POSTGRES_HOST"))
	if host == "" {
		host = "db"
	}

	port := strings.TrimSpace(os.Getenv("POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}

	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	if user == "" {
		user = "postgres"
	}

	password := strings.TrimSpace(os.Getenv("POSTGRES_PASSWORD"))
	if password == "" {
		password = "postgres"
	}

	dbname := strings.TrimSpace(os.Getenv("POSTGRES_DB"))
	if dbname == "" {
		dbname = "x402_scheduler_db"
	}

	sslmode := strings.TrimSpace(os.Getenv("POSTGRES_SSLMODE"))
	if sslmode == "" {
		sslmode = "disable"
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user,
		password,
		host,
		port,
		dbname,
		sslmode,
	)
}

func loadAdminAPIToken() string {
	return strings.TrimSpace(os.Getenv("ADMIN_API_TOKEN"))
}

func loadMaxResultPayloadBytes() int64 {
	const defaultLimit = int64(10 * 1024 * 1024) // 10 MiB

	raw := strings.TrimSpace(os.Getenv("MAX_RESULT_PAYLOAD_BYTES"))
	if raw == "" {
		log.Printf("max result payload bytes: %d (default)", defaultLimit)
		return defaultLimit
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1024 {
		log.Printf("invalid MAX_RESULT_PAYLOAD_BYTES=%q, using %d", raw, defaultLimit)
		return defaultLimit
	}

	log.Printf("max result payload bytes: %d", n)
	return n
}
