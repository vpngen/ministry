package ministry

import (
	"net/netip"

	realmadmin "github.com/vpngen/realm-admin"
	"github.com/vpngen/wordsgens/namesgenerator"
)

type Answer struct {
	realmadmin.Answer
	KeydeskIPv6 netip.Addr            `json:"keydesk_ipv6"`
	FreeSlots   int                   `json:"free_slots"`
	Name        string                `json:"name,omitempty"`
	Mnemo       string                `json:"mnemo.omitempty"`
	Person      namesgenerator.Person `json:"person.omitempty"`
}
