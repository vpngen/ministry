package main

import (
	"context"

	"github.com/google/uuid"
)

// PaymentProvider abstracts the PRO checkout/payment flow. VIP payment
// never goes through this - it already has a real, working pipeline (the
// external VIP partner server polled by ckvip's fetchPaidUsers, reached via
// the existing POST /reserve). Tribute's API for PRO isn't published yet, so
// this stays stubbed the same way KeyIssuer/BillingQuoter are stubbed.
type PaymentProvider interface {
	CreateCheckout(ctx context.Context, brigadeID uuid.UUID, tier, period string) (checkoutRef string, err error)
	ConfirmPayment(ctx context.Context, checkoutRef string) (bool, error)
}

// stubPaymentProvider auto-confirms every checkout, letting the PRO
// create-brigade and upgrade flows be exercised end-to-end before a real
// Tribute integration exists.
type stubPaymentProvider struct{}

func (s *stubPaymentProvider) CreateCheckout(ctx context.Context, brigadeID uuid.UUID, tier, period string) (string, error) {
	return "stub-checkout-" + uuid.New().String(), nil
}

func (s *stubPaymentProvider) ConfirmPayment(ctx context.Context, checkoutRef string) (bool, error) {
	return true, nil
}
