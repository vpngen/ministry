package ministry

import (
	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/wordsgens/namesgenerator"
)

type Answer struct {
	dcmgmt.Answer
	Name   string                `json:"name,omitempty"`
	Mnemo  string                `json:"mnemo.omitempty"`
	Person namesgenerator.Person `json:"person.omitempty"`
}
