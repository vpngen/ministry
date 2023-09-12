package ministry

import (
	realmadmin "github.com/vpngen/realm-admin"
	"github.com/vpngen/wordsgens/namesgenerator"
)

type Answer struct {
	realmadmin.Answer
	Name   string                `json:"name,omitempty"`
	Mnemo  string                `json:"mnemo.omitempty"`
	Person namesgenerator.Person `json:"person.omitempty"`
}
