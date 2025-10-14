package main

import (
	"time"

	"github.com/google/uuid"
)

type PaidUser struct {
	UserID             uuid.UUID `json:"user_id"`
	GoooID             string    `json:"gooo_id"`
	GoodExpiryDateTime time.Time `json:"good_expiry_datetime"`
	ProductPrice       string    `json:"product_price"`
	ProductCurrency    string    `json:"product_currency"`
}

type PaidUsersPesponse struct {
	Result        string     `json:"result"`
	ExecutionTime float64    `json:"execution_time"`
	Data          []PaidUser `json:"data"`
}

type VipBrigade struct {
	RawBrigadeID uuid.UUID `json:"raw_brigade_id"`
	BrigadeID    uuid.UUID `json:"brigade_id"`
	ExpiredAt    time.Time `json:"expiration"`
	UsersCount   int       `json:"users_count"`
}
