package geocoding_repository

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

const (
	answerLockTimeout = "2s"

	answerLockTimeoutSQL = "SELECT set_config('lock_timeout', ?, true)"

	readAnswersSQL = "SELECT c.workspace_id::text AS workspace_id, c.fingerprint, c.outcome, c.latitude, c.longitude, c.geo_precision, c.provider, c.resolved_at" +
		" FROM geocode_cache c JOIN unnest(?::uuid[], ?::text[]) AS k(workspace_id, fingerprint)" +
		" ON c.workspace_id = k.workspace_id AND c.fingerprint = k.fingerprint"

	rememberAnswerSQL = "INSERT INTO geocode_cache (workspace_id, fingerprint, outcome, latitude, longitude, geo_precision, provider, resolved_at)" +
		" SELECT ?::uuid, ?, ?, ?::float8, ?::float8, ?, ?, ?::timestamptz" +
		" WHERE EXISTS (SELECT 1 FROM lead_addresses a JOIN leads holder ON holder.id = a.lead_id AND holder.deleted_at IS NULL" +
		" WHERE a.workspace_id = ?::uuid AND a.fingerprint = ? FOR SHARE OF a, holder)" +
		" ON CONFLICT (workspace_id, fingerprint) DO UPDATE SET outcome = excluded.outcome, latitude = excluded.latitude," +
		" longitude = excluded.longitude, geo_precision = excluded.geo_precision, provider = excluded.provider, resolved_at = excluded.resolved_at"
)

var errAnswerInvalid = errors.New("geocode cache: only a valid provider answer about an address text is stored")

type answerRow struct {
	WorkspaceID  string
	Fingerprint  string
	Outcome      string
	Latitude     *float64
	Longitude    *float64
	GeoPrecision *string
	Provider     string
	ResolvedAt   time.Time
}

func (r answerRow) answer() geocoding.Answer {
	a := geocoding.Answer{
		Key:      geocoding.AnswerKey{WorkspaceID: r.WorkspaceID, Fingerprint: r.Fingerprint},
		Provider: geocoding.Provider(r.Provider), Kind: geocoding.AnswerKind(r.Outcome), ResolvedAt: r.ResolvedAt,
	}
	if r.Latitude != nil && r.Longitude != nil && r.GeoPrecision != nil {
		a.Fix = &geo.Fix{
			Point: geo.Point{Lat: *r.Latitude, Lng: *r.Longitude}, Precision: geo.Precision(*r.GeoPrecision),
			Source: geo.SourceProvider, Provider: r.Provider, FixedAt: r.ResolvedAt,
		}
	}
	return a
}

type AnswerStore struct {
	db *gorm.DB
}

var _ geocoding.AnswerCache = (*AnswerStore)(nil)

func NewAnswerStore(db *gorm.DB) *AnswerStore {
	return &AnswerStore{db: db}
}

func (s *AnswerStore) Answers(ctx context.Context, keys []geocoding.AnswerKey) (map[geocoding.AnswerKey]geocoding.Answer, error) {
	keys = geocoding.AnswerKeysOf(keys)
	out := make(map[geocoding.AnswerKey]geocoding.Answer, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	workspaces, fingerprints := make(pq.StringArray, len(keys)), make(pq.StringArray, len(keys))
	for i, k := range keys {
		workspaces[i], fingerprints[i] = k.WorkspaceID, k.Fingerprint
	}
	var rows []answerRow
	if err := s.db.WithContext(ctx).Raw(readAnswersSQL, workspaces, fingerprints).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		answer := row.answer()
		if !answer.Valid() {
			log.Printf("[geocoding] a stored answer of workspace %s is not usable and is ignored", row.WorkspaceID)
			continue
		}
		out[answer.Key] = answer
	}
	return out, nil
}

func (s *AnswerStore) Remember(ctx context.Context, answer geocoding.Answer) error {
	if !answer.Valid() {
		return errAnswerInvalid
	}
	var lat, lng, precision any
	if answer.Fix != nil {
		lat, lng, precision = answer.Fix.Point.Lat, answer.Fix.Point.Lng, string(answer.Fix.Precision)
	}
	k := answer.Key
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(answerLockTimeoutSQL, answerLockTimeout).Error; err != nil {
			return err
		}
		return tx.Exec(rememberAnswerSQL,
			k.WorkspaceID, k.Fingerprint, string(answer.Kind), lat, lng, precision, string(answer.Provider), answer.ResolvedAt.UTC(),
			k.WorkspaceID, k.Fingerprint,
		).Error
	})
}
