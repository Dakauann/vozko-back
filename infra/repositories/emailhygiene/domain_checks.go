package emailhygiene_repository

import (
	"context"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/emailhygiene"
	"vozko/infra/database"
)

const (
	domainRequestSQL = `INSERT INTO email_domain_checks AS c (domain, state, detail, expires_at, due_at, attempts, created_at, updated_at)
SELECT d, ?, '', ?, ?, 0, ?, ? FROM unnest(?::text[]) AS d
ON CONFLICT (domain) DO UPDATE SET due_at = EXCLUDED.due_at, updated_at = EXCLUDED.updated_at
WHERE c.due_at IS NULL AND c.expires_at <= EXCLUDED.updated_at`

	domainClaimSQL = `UPDATE email_domain_checks SET due_at = ?, updated_at = ?
WHERE domain IN (SELECT domain FROM email_domain_checks WHERE due_at IS NOT NULL AND due_at <= ? ORDER BY due_at LIMIT ? FOR UPDATE SKIP LOCKED)
RETURNING domain, state, detail, checked_at, expires_at, attempts`

	domainSaveSQL = `INSERT INTO email_domain_checks AS c (domain, state, detail, checked_at, expires_at, due_at, attempts, created_at, updated_at)
SELECT v.domain, v.state, v.detail, ?, v.expires_at::timestamptz, NULLIF(v.due_at, '')::timestamptz, v.attempts, ?, ?
FROM unnest(?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::int[]) AS v(domain, state, detail, expires_at, due_at, attempts)
ON CONFLICT (domain) DO UPDATE SET state = EXCLUDED.state, detail = EXCLUDED.detail, checked_at = EXCLUDED.checked_at,
  expires_at = EXCLUDED.expires_at, due_at = EXCLUDED.due_at, attempts = EXCLUDED.attempts, updated_at = EXCLUDED.updated_at`

	domainPruneSQL = `DELETE FROM email_domain_checks WHERE ctid = ANY(ARRAY(
SELECT ctid FROM email_domain_checks WHERE updated_at < ? AND due_at IS NULL ORDER BY updated_at LIMIT ?))`
)

type DomainChecks struct {
	db *gorm.DB
}

var _ emailhygiene.DomainCheckStore = (*DomainChecks)(nil)

func NewDomainChecks(db *gorm.DB) *DomainChecks {
	return &DomainChecks{db: db}
}

type domainRow struct {
	Domain    string
	State     string
	Detail    string
	CheckedAt *time.Time
	ExpiresAt time.Time
	Attempts  int
}

func (r domainRow) check() emailhygiene.DomainCheck {
	state := emailhygiene.DomainState(r.State)
	if !state.Valid() {
		state = emailhygiene.DomainUnknown
	}
	return emailhygiene.DomainCheck{Domain: r.Domain, State: state, Detail: emailhygiene.DomainDetail(r.Detail), CheckedAt: r.CheckedAt, ExpiresAt: r.ExpiresAt, Attempts: r.Attempts}
}

func (s *DomainChecks) Read(ctx context.Context, domains []string) (map[string]emailhygiene.DomainCheck, error) {
	out := map[string]emailhygiene.DomainCheck{}
	if len(domains) == 0 {
		return out, nil
	}
	var rows []domainRow
	err := s.db.WithContext(ctx).Raw(`SELECT domain, state, detail, checked_at, expires_at, attempts FROM email_domain_checks WHERE domain = ANY(?::text[])`,
		database.Texts(unique(domains))).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Domain] = r.check()
	}
	return out, nil
}

func (s *DomainChecks) Request(ctx context.Context, domains []string, now time.Time) (int64, error) {
	if len(domains) == 0 {
		return 0, nil
	}
	now = now.UTC()
	res := s.db.WithContext(ctx).Exec(domainRequestSQL, string(emailhygiene.DomainPending), now, now, now, now, database.Texts(unique(domains)))
	return res.RowsAffected, res.Error
}

func (s *DomainChecks) ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]emailhygiene.DomainCheck, error) {
	now = now.UTC()
	var rows []domainRow
	if err := s.db.WithContext(ctx).Raw(domainClaimSQL, now.Add(lease), now, now, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]emailhygiene.DomainCheck, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.check())
	}
	return out, nil
}

func (s *DomainChecks) Save(ctx context.Context, results []emailhygiene.DomainResult, now time.Time) error {
	if len(results) == 0 {
		return nil
	}
	now = now.UTC()
	n := len(results)
	domains, states, details, expires, dues := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	attempts := make([]int64, n)
	for i, r := range results {
		domains[i], states[i], details[i] = r.Domain, string(r.Verdict.State), string(r.Verdict.Detail)
		expires[i] = r.Settled.ExpiresAt.UTC().Format(time.RFC3339Nano)
		if r.Settled.DueAt != nil {
			dues[i] = r.Settled.DueAt.UTC().Format(time.RFC3339Nano)
		}
		attempts[i] = int64(r.Attempts)
	}
	return s.db.WithContext(ctx).Exec(domainSaveSQL, now, now, now, database.Texts(domains), database.Texts(states), database.Texts(details),
		database.Texts(expires), database.Texts(dues), pq.Int64Array(attempts)).Error
}

func (s *DomainChecks) Prune(ctx context.Context, idleBefore time.Time, limit int) (int64, error) {
	res := s.db.WithContext(ctx).Exec(domainPruneSQL, idleBefore.UTC(), limit)
	return res.RowsAffected, res.Error
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok || v == "" {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
