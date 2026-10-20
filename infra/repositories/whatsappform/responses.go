package whatsappform_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

var (
	errMatchesNothing         = errors.New("whatsappform repository: the filter matches nothing")
	errInboundOfAnotherTenant = errors.New("whatsappform repository: the inbound message belongs to another workspace")
)

const responseColumns = "r.id::text AS id, r.workspace_id::text AS workspace_id, r.form_id::text AS form_id, r.version_id::text AS version_id," +
	" r.send_id::text AS send_id, r.origin, r.match, r.inbound_message_id, r.conversation_id, r.entry_id::text AS entry_id, r.entry_type," +
	" r.lead_id::text AS lead_id, r.business_phone_id::text AS business_phone_id, r.contact_number, r.answers::text AS answers," +
	" r.raw::text AS raw, r.apply_status, r.report::text AS report, r.submitted_at, r.created_at"

const (
	insertResponseSQL = "INSERT INTO whatsapp_form_responses (id, workspace_id, form_id, version_id, send_id, origin, match, inbound_message_id, conversation_id," +
		" entry_id, entry_type, lead_id, business_phone_id, contact_number, answers, raw, apply_status, report, submitted_at, created_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?::jsonb, ?, ?)" +
		" ON CONFLICT (inbound_message_id) DO NOTHING RETURNING id::text"

	responseByInboundSQL = "SELECT " + responseColumns + " FROM whatsapp_form_responses r WHERE r.inbound_message_id = ?"

	saveApplySQL = "UPDATE whatsapp_form_responses SET apply_status = ?, report = ?::jsonb WHERE workspace_id = ? AND id = ?"

	deleteResponseSQL = "DELETE FROM whatsapp_form_responses WHERE workspace_id = ? AND id = ?"

	leadJoinSQL = " LEFT JOIN leads l ON l.id = r.lead_id AND l.workspace_id = r.workspace_id AND l.deleted_at IS NULL"
)

var scopedResponsesSQL = "SELECT " + responseColumns + ", wc.id::text AS campaign_id FROM whatsapp_form_responses r" + joinsFor("r")

var responseSearchSQL = " AND (r.contact_number LIKE ? OR " + database.SearchFold("l.name") + " LIKE " + database.SearchFold("?") + ")"

type ResponseStore struct {
	db *gorm.DB
}

var _ whatsappform.ResponseRepository = (*ResponseStore)(nil)

func NewResponseStore(db *gorm.DB) *ResponseStore {
	return &ResponseStore{db: db}
}

type responseRow struct {
	ID               string
	WorkspaceID      string
	FormID           schema.OptionalText
	VersionID        schema.OptionalText
	SendID           schema.OptionalText
	Origin           schema.OptionalText
	Match            string
	InboundMessageID string
	ConversationID   string
	EntryID          schema.OptionalText
	EntryType        string
	LeadID           schema.OptionalText
	CampaignID       schema.OptionalText
	BusinessPhoneID  string
	ContactNumber    string
	Answers          string
	Raw              schema.OptionalText
	ApplyStatus      string
	Report           string
	SubmittedAt      time.Time
	CreatedAt        time.Time
}

func (r responseRow) toDomain() (whatsappform.Response, error) {
	response := whatsappform.Response{
		ID: r.ID, WorkspaceID: r.WorkspaceID, FormID: string(r.FormID), VersionID: string(r.VersionID), SendID: string(r.SendID),
		Origin: whatsappform.SendOrigin(r.Origin), Match: whatsappform.MatchResult(r.Match), InboundMessageID: r.InboundMessageID,
		ConversationID: r.ConversationID, EntryID: string(r.EntryID), EntryType: r.EntryType, LeadID: string(r.LeadID), CampaignID: string(r.CampaignID),
		BusinessPhoneID: r.BusinessPhoneID, ContactNumber: r.ContactNumber, ApplyStatus: whatsappform.ApplyStatus(r.ApplyStatus),
		SubmittedAt: r.SubmittedAt, CreatedAt: r.CreatedAt,
	}
	if r.Raw != "" {
		response.Raw = json.RawMessage(r.Raw)
	}
	if err := decodeJSON(r.Answers, &response.Answers); err != nil {
		return whatsappform.Response{}, err
	}
	if err := decodeJSON(r.Report, &response.Report); err != nil {
		return whatsappform.Response{}, err
	}
	return response, nil
}

func (s *ResponseStore) Insert(ctx context.Context, r whatsappform.Response) (whatsappform.Response, bool, error) {
	if strings.TrimSpace(r.InboundMessageID) == "" {
		return whatsappform.Response{}, false, fmt.Errorf("%w: the inbound message is missing", whatsappform.ErrResponseInvalid)
	}
	if !database.AllUUIDs(r.ID, r.WorkspaceID, r.BusinessPhoneID) {
		return whatsappform.Response{}, false, errIDsInvalid
	}
	answers, err := jsonText(r.Answers, "{}")
	if err != nil {
		return whatsappform.Response{}, false, err
	}
	report, err := jsonText(r.Report, "{}")
	if err != nil {
		return whatsappform.Response{}, false, err
	}
	db := s.db.WithContext(ctx)
	var ids []string
	err = db.Raw(insertResponseSQL, r.ID, r.WorkspaceID, schema.OptionalText(r.FormID), schema.OptionalText(r.VersionID), schema.OptionalText(r.SendID),
		schema.OptionalText(r.Origin), string(r.Match), r.InboundMessageID, r.ConversationID, schema.OptionalText(r.EntryID), r.EntryType,
		schema.OptionalText(r.LeadID), r.BusinessPhoneID, r.ContactNumber, answers, rawJSON(r.Raw), string(r.ApplyStatus), report,
		r.SubmittedAt, r.CreatedAt).Scan(&ids).Error
	if err != nil {
		return whatsappform.Response{}, false, err
	}
	if len(ids) == 1 {
		return r, true, nil
	}
	existing, err := s.responses(ctx, responseByInboundSQL, r.InboundMessageID)
	if err != nil {
		return whatsappform.Response{}, false, err
	}
	if len(existing) == 0 {
		return whatsappform.Response{}, false, whatsappform.ErrResponseNotFound
	}
	if existing[0].WorkspaceID != r.WorkspaceID {
		return whatsappform.Response{}, false, errInboundOfAnotherTenant
	}
	return existing[0], false, nil
}

func (s *ResponseStore) SaveApply(ctx context.Context, workspaceID, responseID string, status whatsappform.ApplyStatus, report whatsappform.ApplyReport) error {
	if !database.AllUUIDs(workspaceID, responseID) {
		return whatsappform.ErrResponseNotFound
	}
	text, err := jsonText(report, "{}")
	if err != nil {
		return err
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(saveApplySQL, string(status), text, workspaceID, responseID), whatsappform.ErrResponseNotFound)
}

func (s *ResponseStore) Delete(ctx context.Context, workspaceID, responseID string) error {
	if !database.AllUUIDs(workspaceID, responseID) {
		return whatsappform.ErrResponseNotFound
	}
	return database.AffectedOne(s.db.WithContext(ctx).Exec(deleteResponseSQL, workspaceID, responseID), whatsappform.ErrResponseNotFound)
}

func (s *ResponseStore) Get(ctx context.Context, workspaceID, responseID string, scope whatsappform.ReadScope) (*whatsappform.Response, error) {
	clause, scopeArgs, err := scopeClause(scope)
	if err != nil {
		return nil, err
	}
	if !database.AllUUIDs(workspaceID, responseID) {
		return nil, whatsappform.ErrResponseNotFound
	}
	filter := where(scopedResponsesSQL+" WHERE r.workspace_id = ? AND r.id = ?", workspaceID, responseID).and(clause, scopeArgs...)
	found, err := s.responses(ctx, filter.String(), filter.args...)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, whatsappform.ErrResponseNotFound
	}
	return &found[0], nil
}

func (s *ResponseStore) List(ctx context.Context, q whatsappform.ResponseQuery) (whatsappform.Page[whatsappform.Response], error) {
	empty := whatsappform.Page[whatsappform.Response]{Items: []whatsappform.Response{}}
	if !q.Scope.Resolved {
		return empty, whatsappform.ErrForbidden
	}
	if err := q.Normalize(); err != nil {
		return empty, err
	}
	cursor, err := cursorOf(q.After)
	if err != nil {
		return empty, err
	}
	sql, args, err := responseListQuery(q, cursor)
	if errors.Is(err, errMatchesNothing) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	responses, err := s.responses(ctx, sql, args...)
	if err != nil {
		return empty, err
	}
	return pageOf(responses, q.Limit, func(r whatsappform.Response) shared.Keyset {
		return shared.Keyset{At: r.SubmittedAt, ID: r.ID}
	}), nil
}

func responseListQuery(q whatsappform.ResponseQuery, cursor *shared.Keyset) (string, []interface{}, error) {
	scope, scopeArgs, err := scopeClause(q.Scope)
	if err != nil {
		return "", nil, err
	}
	if !database.AllUUIDs(q.WorkspaceID, q.FormID) || (q.CampaignID != "" && !database.AllUUIDs(q.CampaignID)) {
		return "", nil, errMatchesNothing
	}
	versions, phones := database.UUIDArray(q.VersionIDs), database.UUIDArray(q.PhoneIDs)
	if (len(q.VersionIDs) > 0 && len(versions) == 0) || (len(q.PhoneIDs) > 0 && len(phones) == 0) {
		return "", nil, errMatchesNothing
	}
	from := scopedResponsesSQL
	if q.Search != "" {
		from += leadJoinSQL
	}
	filter := where(from+" WHERE r.workspace_id = ? AND r.form_id = ?", q.WorkspaceID, q.FormID)
	if len(versions) > 0 {
		filter.and(" AND r.version_id = ANY(?::uuid[])", versions)
	}
	filter.within("r.submitted_at", q.From, q.To)
	if len(q.Origins) > 0 {
		filter.and(" AND r.origin = ANY(?)", pq.Array(originTexts(q.Origins)))
	}
	if len(phones) > 0 {
		filter.and(" AND r.business_phone_id = ANY(?::uuid[])", phones)
	}
	if q.CampaignID != "" {
		filter.and(" AND wce.campaign_id = ?", q.CampaignID)
	}
	filter.and(matchClause(q.Match))
	if q.Search != "" {
		pattern := sqlexpr.LikeContains(q.Search)
		filter.and(responseSearchSQL, pattern, pattern)
	}
	filter.and(scope, scopeArgs...)
	if cursor != nil {
		filter.and(" AND (r.submitted_at, r.id) < (?, ?::uuid)", cursor.At, cursor.ID)
	}
	filter.and(" ORDER BY r.submitted_at DESC, r.id DESC LIMIT ?", q.Limit+1)
	return filter.String(), filter.args, nil
}

func matchClause(match whatsappform.MatchFilter) string {
	switch match {
	case whatsappform.MatchFilterMatched:
		return " AND r.match IN (" + matchedResults + ")"
	case whatsappform.MatchFilterUnmatched:
		return " AND r.match NOT IN (" + matchedResults + ")"
	}
	return ""
}

func originTexts(origins []whatsappform.SendOrigin) []string {
	texts := make([]string, len(origins))
	for i, origin := range origins {
		texts[i] = string(origin)
	}
	return texts
}

func (s *ResponseStore) responses(ctx context.Context, sql string, args ...interface{}) ([]whatsappform.Response, error) {
	var rows []responseRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	responses := make([]whatsappform.Response, 0, len(rows))
	for _, row := range rows {
		response, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}
