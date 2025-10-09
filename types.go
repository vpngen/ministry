package ministry

import (
	"github.com/google/uuid"
	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/wordsgens/namesgenerator"
)

type Answer struct {
	dcmgmt.Answer
	Name   string                `json:"name,omitempty"`
	Mnemo  string                `json:"mnemo.omitempty"`
	Person namesgenerator.Person `json:"person.omitempty"`
}

type VIPReserve struct {
	RequestID uuid.UUID `json:"request_id"`
}
