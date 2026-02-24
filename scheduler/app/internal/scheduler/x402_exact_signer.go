// Prepares a signed payment message, so a USDC transfer can be verified.

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	x402evm "github.com/coinbase/x402/go/mechanisms/evm"
	exactclient "github.com/coinbase/x402/go/mechanisms/evm/exact/client"
	evmsigners "github.com/coinbase/x402/go/signers/evm"
	"github.com/coinbase/x402/go/types"
	"github.com/ethereum/go-ethereum/common"
)

type x402ExactSigner struct {
	network        string
	assetAddress   common.Address
	signer         x402evm.ClientEvmSigner
	scheme         *exactclient.ExactEvmScheme
	tokenName      string
	tokenVersion   string
	amountDecimals int
	authValidity   time.Duration
}

type X402Envelope struct {
	X402Version         int `json:"x402Version"`
	PaymentPayload      any `json:"paymentPayload"`
	PaymentRequirements any `json:"paymentRequirements"`
}

func newX402ExactSigner(cfg X402ProviderConfig) (*x402ExactSigner, error) {
	privateKeyHex := strings.TrimSpace(cfg.PayerPrivateKey)
	if privateKeyHex == "" {
		return nil, errors.New("x402 payer private key is required")
	}

	signer, err := evmsigners.NewClientSignerFromPrivateKey(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid x402 payer private key: %w", err)
	}

	network := strings.TrimSpace(cfg.Network)
	if network == "" {
		return nil, errors.New("x402 network is required")
	}

	assetRaw := strings.TrimSpace(cfg.AssetAddress)
	if assetRaw == "" {
		return nil, errors.New("x402 asset address is required")
	}
	if !common.IsHexAddress(assetRaw) {
		return nil, fmt.Errorf("invalid x402 asset address: %s", assetRaw)
	}

	tokenName := strings.TrimSpace(cfg.TokenName)
	if tokenName == "" {
		return nil, errors.New("x402 token name is required")
	}
	tokenVersion := strings.TrimSpace(cfg.TokenVersion)
	if tokenVersion == "" {
		return nil, errors.New("x402 token version is required")
	}

	decimals := cfg.AmountDecimals
	if decimals < 0 {
		return nil, errors.New("invalid token decimals")
	}

	authValidity := cfg.AuthValidity
	if authValidity <= 0 {
		authValidity = 60 * time.Second
	}

	scheme := exactclient.NewExactEvmScheme(signer)

	return &x402ExactSigner{
		network:        network,
		assetAddress:   common.HexToAddress(assetRaw),
		signer:         signer,
		scheme:         scheme,
		tokenName:      tokenName,
		tokenVersion:   tokenVersion,
		amountDecimals: decimals,
		authValidity:   authValidity,
	}, nil
}

func (s *x402ExactSigner) BuildEnvelope(event PaymentEvent) (X402Envelope, error) {
	if !common.IsHexAddress(event.WorkerID) {
		return X402Envelope{}, fmt.Errorf("worker_id must be wallet address: %s", event.WorkerID)
	}
	payTo := common.HexToAddress(event.WorkerID)

	valueUnits, err := x402evm.ParseAmount(strings.TrimSpace(event.AmountUSDC), s.amountDecimals)
	if err != nil {
		return X402Envelope{}, err
	}
	if valueUnits.Sign() <= 0 {
		return X402Envelope{}, fmt.Errorf("amount must be greater than zero: %s", event.AmountUSDC)
	}

	req := types.PaymentRequirements{
		Scheme:            "exact",
		Network:           s.network,
		Asset:             s.assetAddress.Hex(),
		Amount:            valueUnits.String(),
		PayTo:             payTo.Hex(),
		MaxTimeoutSeconds: int(s.authValidity.Seconds()),
		Extra: map[string]interface{}{
			"name":    s.tokenName,
			"version": s.tokenVersion,
		},
	}

	paymentPayload, err := s.scheme.CreatePaymentPayload(context.Background(), req)
	if err != nil {
		return X402Envelope{}, err
	}
	paymentPayload.Accepted = req

	return X402Envelope{
		X402Version:         paymentPayload.X402Version,
		PaymentPayload:      paymentPayload,
		PaymentRequirements: req,
	}, nil
}
