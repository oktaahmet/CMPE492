//go:build !unit

// Calls the facilitator /verify and /settle API endpoints.

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	x402http "github.com/coinbase/x402/go/http"
)

type X402ProviderConfig struct {
	BaseURL         string
	CDPAPIKeyID     string
	CDPPrivateKey   string
	Timeout         time.Duration
	Network         string
	AssetAddress    string
	PayerPrivateKey string
	TokenName       string
	TokenVersion    string
	AmountDecimals  int
	AuthValidity    time.Duration
}

type X402PaymentProvider struct {
	exactSigner *x402ExactSigner
	facilitator *x402http.HTTPFacilitatorClient
}

type PaymentReceipt struct {
	TxHash  string `json:"tx_hash,omitempty"`
	Network string `json:"network,omitempty"`
	Payer   string `json:"payer,omitempty"`
}

func NewX402PaymentProvider(cfg X402ProviderConfig) (*X402PaymentProvider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("x402 base url is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid x402 base url: %s", cfg.BaseURL)
	}

	apiKeyID := strings.TrimSpace(cfg.CDPAPIKeyID)
	privateKey := strings.TrimSpace(cfg.CDPPrivateKey)
	jwtSigner, err := newCDPJWTSigner(apiKeyID, privateKey)
	if err != nil {
		return nil, err
	}

	exactSigner, err := newX402ExactSigner(cfg)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: timeout}

	authProvider := &cdpFacilitatorAuthProvider{
		signer:            jwtSigner,
		requestHost:       parsed.Host,
		requestPathPrefix: strings.TrimRight(parsed.EscapedPath(), "/"),
	}
	fac := x402http.NewHTTPFacilitatorClient(&x402http.FacilitatorConfig{
		URL:          baseURL,
		HTTPClient:   httpClient,
		AuthProvider: authProvider,
		Timeout:      timeout,
		Identifier:   baseURL,
	})

	return &X402PaymentProvider{
		exactSigner: exactSigner,
		facilitator: fac,
	}, nil
}

func (x *X402PaymentProvider) Transfer(event PaymentEvent) (PaymentReceipt, error) {
	if x.exactSigner == nil {
		return PaymentReceipt{}, errors.New("x402 exact signer is not configured")
	}
	if x.facilitator == nil {
		return PaymentReceipt{}, errors.New("x402 facilitator client is not configured")
	}

	envelope, err := x.exactSigner.BuildEnvelope(event)
	if err != nil {
		return PaymentReceipt{}, err
	}

	payloadBytes, err := json.Marshal(envelope.PaymentPayload)
	if err != nil {
		return PaymentReceipt{}, fmt.Errorf("marshal paymentPayload: %w", err)
	}
	reqBytes, err := json.Marshal(envelope.PaymentRequirements)
	if err != nil {
		return PaymentReceipt{}, fmt.Errorf("marshal paymentRequirements: %w", err)
	}

	ctx := context.Background()

	verifyResp, err := x.facilitator.Verify(ctx, payloadBytes, reqBytes)
	if err != nil {
		return PaymentReceipt{}, err
	}
	if verifyResp == nil || !verifyResp.IsValid {
		reason := "payment is invalid"
		if verifyResp != nil && verifyResp.InvalidReason != "" {
			reason = verifyResp.InvalidReason
		}
		return PaymentReceipt{}, errors.New(reason)
	}

	settleResp, err := x.facilitator.Settle(ctx, payloadBytes, reqBytes)
	if err != nil {
		return PaymentReceipt{}, err
	}
	if settleResp == nil {
		return PaymentReceipt{}, errors.New("settlement failed: empty response")
	}

	return PaymentReceipt{
		TxHash:  settleResp.Transaction,
		Network: string(settleResp.Network),
		Payer:   settleResp.Payer,
	}, nil
}

type cdpFacilitatorAuthProvider struct {
	signer            *cdpJWTSigner
	requestHost       string
	requestPathPrefix string
}

func (a *cdpFacilitatorAuthProvider) GetAuthHeaders(ctx context.Context) (x402http.AuthHeaders, error) {
	verifyPath := joinRequestPath(a.requestPathPrefix, "/verify")
	settlePath := joinRequestPath(a.requestPathPrefix, "/settle")
	supportedPath := joinRequestPath(a.requestPathPrefix, "/supported")

	verifyJWT, err := a.signer.Sign(http.MethodPost, a.requestHost, verifyPath)
	if err != nil {
		return x402http.AuthHeaders{}, err
	}
	settleJWT, err := a.signer.Sign(http.MethodPost, a.requestHost, settlePath)
	if err != nil {
		return x402http.AuthHeaders{}, err
	}
	supportedJWT, err := a.signer.Sign(http.MethodGet, a.requestHost, supportedPath)
	if err != nil {
		return x402http.AuthHeaders{}, err
	}

	return x402http.AuthHeaders{
		Verify: map[string]string{
			"Authorization": "Bearer " + verifyJWT,
		},
		Settle: map[string]string{
			"Authorization": "Bearer " + settleJWT,
		},
		Supported: map[string]string{
			"Authorization": "Bearer " + supportedJWT,
		},
	}, nil
}

func joinRequestPath(prefix, leaf string) string {
	p := strings.TrimSpace(prefix)
	l := strings.TrimSpace(leaf)

	if p == "" {
		if strings.HasPrefix(l, "/") {
			return l
		}
		return "/" + l
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")

	if l == "" {
		return p
	}
	if !strings.HasPrefix(l, "/") {
		l = "/" + l
	}
	return p + l
}
