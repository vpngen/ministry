package core

import (
	"context"
	"fmt"
	"net/netip"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"golang.org/x/crypto/ssh"

	dcmgmt "github.com/vpngen/dc-mgmt"
)

// Mock UUIDs are fixed so that repeated mock runs are idempotent.
var (
	MockRealmID   = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	MockPartnerID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
)

// ensureMockRealm upserts a mock realm record and links it to the brigade's
// partner via partners_realms. addr is the control_ip:port of the dc-mgmt
// staging server; if zero, 127.0.0.1:22 is used.
func ensureMockRealm(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID, addr netip.AddrPort) error {
	if !addr.IsValid() {
		addr = netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), DefaultRealmsPort)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	// Upsert mock realm.
	sqlUpsertRealm := `
INSERT INTO head.realms (realm_id, realm_name, control_ip, is_active, open_for_regs, free_slots)
VALUES ($1, 'mock-realm', $2, true, true, 100000)
ON CONFLICT (realm_id) DO UPDATE
    SET is_active = true, open_for_regs = true, free_slots = 100000
`
	if _, err := tx.Exec(ctx, sqlUpsertRealm, MockRealmID, addr.Addr().String()); err != nil {
		return fmt.Errorf("upsert mock realm: %w", err)
	}

	// Resolve the partner_id for this brigade.
	var partnerID uuid.UUID

	sqlGetPartner := `SELECT partner_id FROM head.brigadier_partners WHERE brigade_id = $1 LIMIT 1`
	if err := tx.QueryRow(ctx, sqlGetPartner, brigadeID).Scan(&partnerID); err != nil {
		return fmt.Errorf("get brigade partner: %w", err)
	}

	// Link partner → mock realm (idempotent).
	sqlUpsertLink := `
INSERT INTO head.partners_realms (partner_id, realm_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING
`
	if _, err := tx.Exec(ctx, sqlUpsertLink, partnerID, MockRealmID); err != nil {
		return fmt.Errorf("upsert partners_realms: %w", err)
	}

	return tx.Commit(ctx)
}

// ComposeBrigadeMock is the mock-aware version of ComposeBrigade.  It:
//  1. Seeds a mock realm in the ministry DB pointing at addr (the staging dc-mgmt).
//  2. Calls the normal ComposeBrigade flow, but appends -mock to the addbrigade
//     command so dc-mgmt also seeds its own mock prerequisites.
//  3. Falls back to a static hardcoded config if the SSH call fails, so the
//     rest of the ministry flow still completes even when dc-mgmt is unreachable.
func ComposeBrigadeMock(
	ctx context.Context,
	db *pgxpool.Pool,
	sshconf *ssh.ClientConfig,
	tag string,
	brigadeID uuid.UUID,
	fullname string,
	person *namesgenerator.Person,
	addr netip.AddrPort,
) (*dcmgmt.Answer, error) {
	if err := ensureMockRealm(ctx, db, brigadeID, addr); err != nil {
		return nil, fmt.Errorf("ensure mock realm: %w", err)
	}

	attempts := 0

	for {
		if attempts++; attempts > RealmSelectMaxAttempts {
			break
		}

		realmID, realmAddr, err := DefineBrigadeRealm(ctx, db, brigadeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: mock compose: define realm: %s\n", tag, err)

			break
		}

		vpnconf, err := callRealmAddBrigade(ctx, sshconf, tag, realmID, realmAddr, false, true, brigadeID, fullname, person)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: mock compose: call realm: %s\n", tag, err)

			break
		}

		if err := promoteBrigadierRealm(ctx, db, brigadeID, realmID); err != nil {
			fmt.Fprintf(os.Stderr, "%s: mock compose: promote realm: %s\n", tag, err)

			break
		}

		return vpnconf, nil
	}

	// Fallback: SSH to dc-mgmt failed or dc-mgmt not reachable — return a
	// static mock answer so the ministry DB write still succeeds.
	fmt.Fprintf(os.Stderr, "%s: mock compose: falling back to static mock config for brigade %s\n", tag, brigadeID)

	return MockComposeBrigade(tag, brigadeID)
}
