package main

import (
	"net/netip"

	"github.com/google/uuid"
)

type Realm struct {
	RealmID   uuid.UUID
	ControlIP netip.AddrPort
}
