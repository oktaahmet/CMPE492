//go:build unit

package scheduler

import "time"

type PaymentReceipt struct {
	TxHash  string `json:"tx_hash,omitempty"`
	Network string `json:"network,omitempty"`
	Payer   string `json:"payer,omitempty"`
}

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

type X402PaymentProvider struct{}

func NewX402PaymentProvider(cfg X402ProviderConfig) (*X402PaymentProvider, error) {
	return &X402PaymentProvider{}, nil
}

func (x *X402PaymentProvider) Transfer(event PaymentEvent) (PaymentReceipt, error) {
	return PaymentReceipt{}, nil
}
