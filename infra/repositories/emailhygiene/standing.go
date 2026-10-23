package emailhygiene_repository

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/infra/database"
)

const (
	hitSuppression = "s"
	hitBadAddress  = "b"
	hitDomain      = "d"
	hitActivity    = "x"
)

const StandingSQL = `SELECT 's' AS kind, address AS key, reason AS state, suppressed_at AS stamp, NULL::timestamptz AS first_delivered_at,
  NULL::timestamptz AS last_delivered_at, NULL::timestamptz AS last_engaged_at, NULL::timestamptz AS sunset_since,
  0 AS campaigns_since_engaged, 0 AS soft_streak, NULL::timestamptz AS soft_streak_started_at
FROM email_suppressions WHERE workspace_id = ?::uuid AND address = ANY(?::text[])
UNION ALL
SELECT 'b', encode(address_hash, 'hex'), reason, first_seen_at, NULL, NULL, NULL, NULL, 0, 0, NULL
FROM email_bad_addresses WHERE address_hash = ANY(?::bytea[])
UNION ALL
SELECT 'd', domain, state, expires_at, NULL, NULL, NULL, NULL, 0, 0, NULL
FROM email_domain_checks WHERE domain = ANY(?::text[])
UNION ALL
SELECT 'x', encode(address_hash, 'hex'), '', touched_at, first_delivered_at, last_delivered_at, last_engaged_at, sunset_since,
  campaigns_since_engaged, soft_streak, soft_streak_started_at
FROM email_address_activity WHERE workspace_id = ?::uuid AND address_hash = ANY(?::bytea[])`

type Standing struct {
	db  *gorm.DB
	now func() time.Time
}

var _ emailhygiene.StandingReader = (*Standing)(nil)

func NewStanding(db *gorm.DB) *Standing {
	return &Standing{db: db, now: func() time.Time { return time.Now().UTC() }}
}

type hitRow struct {
	Kind                  string
	Key                   string
	State                 string
	Stamp                 *time.Time
	FirstDeliveredAt      *time.Time
	LastDeliveredAt       *time.Time
	LastEngagedAt         *time.Time
	SunsetSince           *time.Time
	CampaignsSinceEngaged int
	SoftStreak            int
	SoftStreakStartedAt   *time.Time
}

type StandingInput struct {
	Addresses []emailaddress.Address
	Raw       database.Texts
	Hashes    pq.ByteaArray
	Domains   database.Texts
	hexOf     map[emailaddress.Address]string
}

func StandingArgs(addresses []emailaddress.Address) StandingInput {
	in := StandingInput{hexOf: make(map[emailaddress.Address]string, len(addresses))}
	domains := map[string]struct{}{}
	for _, a := range addresses {
		if _, ok := in.hexOf[a]; ok || a == "" {
			continue
		}
		hash := emailhygiene.HashOf(a)
		in.hexOf[a] = hex.EncodeToString(hash[:])
		in.Addresses = append(in.Addresses, a)
		in.Raw = append(in.Raw, string(a))
		in.Hashes = append(in.Hashes, append([]byte(nil), hash[:]...))
		if _, ok := domains[a.Domain()]; !ok {
			domains[a.Domain()] = struct{}{}
			in.Domains = append(in.Domains, a.Domain())
		}
	}
	return in
}

func (in StandingInput) Args(workspaceID string) []any {
	return []any{workspaceID, in.Raw, in.Hashes, in.Domains, workspaceID, in.Hashes}
}

func (r *Standing) Standing(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]emailhygiene.Standing, error) {
	out := map[emailaddress.Address]emailhygiene.Standing{}
	in := StandingArgs(addresses)
	if len(in.Addresses) == 0 {
		return out, nil
	}
	var rows []hitRow
	if err := r.db.WithContext(ctx).Raw(StandingSQL, in.Args(workspaceID)...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byKind := map[string]map[string]hitRow{hitSuppression: {}, hitBadAddress: {}, hitDomain: {}, hitActivity: {}}
	for _, row := range rows {
		if hits, ok := byKind[row.Kind]; ok {
			hits[row.Key] = row
		}
	}
	now := r.now()
	for _, a := range in.Addresses {
		out[a] = composeStanding(byKind, string(a), in.hexOf[a], a.Domain(), now)
	}
	return out, nil
}

func composeStanding(byKind map[string]map[string]hitRow, address, hashHex, domain string, now time.Time) emailhygiene.Standing {
	s := emailhygiene.Standing{Domain: emailhygiene.DomainPending, DomainStale: true}
	if row, ok := byKind[hitSuppression][address]; ok {
		s.Suppression, s.SuppressedAt = es.SuppressReason(row.State), row.Stamp
	}
	_, s.BadAddress = byKind[hitBadAddress][hashHex]
	if row, ok := byKind[hitDomain][domain]; ok {
		if state := emailhygiene.DomainState(row.State); state.Valid() {
			s.Domain = state
		}
		s.DomainStale = s.Domain == emailhygiene.DomainPending || row.Stamp == nil || !row.Stamp.After(now)
	}
	if row, ok := byKind[hitActivity][hashHex]; ok {
		s.Activity = emailhygiene.Activity{
			Known:                 true,
			FirstDeliveredAt:      row.FirstDeliveredAt,
			LastDeliveredAt:       row.LastDeliveredAt,
			LastEngagedAt:         row.LastEngagedAt,
			SunsetSince:           row.SunsetSince,
			CampaignsSinceEngaged: row.CampaignsSinceEngaged,
			SoftStreak:            row.SoftStreak,
			SoftStreakStartedAt:   row.SoftStreakStartedAt,
		}
	}
	return s
}

func RefusedSQL(workspaceExpr, addressExpr string) string {
	return `(EXISTS (SELECT 1 FROM email_suppressions WHERE workspace_id = ` + workspaceExpr + ` AND address = ` + addressExpr + `)
	  OR EXISTS (SELECT 1 FROM email_bad_addresses WHERE address_hash = sha256(convert_to(` + addressExpr + `, 'UTF8')))
	  OR EXISTS (SELECT 1 FROM email_domain_checks WHERE domain = split_part(` + addressExpr + `, '@', 2) AND state = '` + string(emailhygiene.DomainNoMail) + `'))`
}
