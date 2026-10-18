package customerportfolio_repository

import (
	"context"
	"strings"

	cp "vozko/domain/customerportfolio"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/sqlexpr"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

type displayColumns struct {
	name, handle, number, avatar string
}

var leadDisplay = displayColumns{
	name: "NULLIF(l.name, '')", handle: "NULL", number: "NULLIF(l.number, '')", avatar: "NULLIF(l.profile_picture_url, '')",
}

var contactDisplays = []struct {
	channel shared.EntryType
	columns func(a string) displayColumns
}{
	{shared.EntryTypeUnofficialWhatsApp, func(a string) displayColumns {
		return displayColumns{
			name:   "COALESCE(NULLIF(" + a + ".contact_name, ''), NULLIF(" + a + ".name, ''), NULLIF(" + a + ".verified_name, ''))",
			handle: "NULL", number: "NULLIF(" + a + ".phone_number, '')", avatar: "NULLIF(" + a + ".picture_url, '')",
		}
	}},
	{shared.EntryTypeTelegram, func(a string) displayColumns {
		return displayColumns{
			name:   fullName(a),
			handle: "NULLIF(" + a + ".username, '')", number: "NULLIF(" + a + ".phone_number, '')", avatar: "NULLIF(" + a + ".photo_url, '')",
		}
	}},
	{shared.EntryTypeInstagram, func(a string) displayColumns {
		return displayColumns{
			name:   "NULLIF(" + a + ".name, '')",
			handle: "NULLIF(" + a + ".username, '')", number: "NULL", avatar: "NULLIF(" + a + ".profile_picture_url, '')",
		}
	}},
	{shared.EntryTypeFacebook, func(a string) displayColumns {
		return displayColumns{
			name:   "COALESCE(NULLIF(" + a + ".name, ''), " + fullName(a) + ")",
			handle: "NULL", number: "NULL", avatar: "NULL",
		}
	}},
	{shared.EntryTypeWebchat, func(a string) displayColumns {
		return displayColumns{
			name:   "NULLIF(" + a + ".name, '')",
			handle: "NULLIF(" + a + ".email, '')", number: "NULLIF(" + a + ".phone, '')", avatar: "NULL",
		}
	}},
}

func fullName(a string) string {
	return "NULLIF(btrim(concat_ws(' ', " + a + ".first_name, " + a + ".last_name)), '')"
}

var listRowsSQL = buildListRowsSQL()

func buildListRowsSQL() string {
	names, handles, numbers, avatars := []string{leadDisplay.name}, []string{leadDisplay.handle}, []string{leadDisplay.number}, []string{leadDisplay.avatar}
	joins := " LEFT JOIN leads l ON l.id = p.lead_id AND l.workspace_id = p.workspace_id"
	for _, d := range contactDisplays {
		channel, ok := infracrmfilter.ContactsOf(d.channel)
		if !ok {
			panic("customer portfolio: no contact table for channel " + string(d.channel))
		}
		alias := "ct_" + string(d.channel)
		joins += " LEFT JOIN " + channel.Contacts + " " + alias + " ON p.contact_type = '" + string(d.channel) + "' AND " + alias + ".id = p.contact_id"
		columns := d.columns(alias)
		names, handles = append(names, columns.name), append(handles, columns.handle)
		numbers, avatars = append(numbers, columns.number), append(avatars, columns.avatar)
	}
	return "SELECT " + entryColumns +
		", COALESCE(" + strings.Join(names, ", ") + ") AS display_name" +
		", COALESCE(" + strings.Join(handles, ", ") + ") AS display_handle" +
		", COALESCE(" + strings.Join(numbers, ", ") + ") AS display_number" +
		", COALESCE(" + strings.Join(avatars, ", ") + ") AS display_avatar" +
		" FROM customer_portfolio_entries p" + joins
}

var listSearchSQL = " WHERE " + database.SearchFold("concat_ws(' ', display_name, display_handle, display_number)") + " LIKE " + database.SearchFold("?")

type listRow struct {
	Entry         entryRow `gorm:"embedded"`
	DisplayName   string
	DisplayHandle string
	DisplayNumber string
	DisplayAvatar string
}

func (r listRow) toDomain() cp.Row {
	entry := r.Entry.toDomain()
	return cp.Row{
		Entry: entry,
		Display: cp.Display{
			Channel: entry.Subject.ContactType, Name: r.DisplayName, Handle: r.DisplayHandle, Number: r.DisplayNumber, AvatarURL: r.DisplayAvatar,
		},
	}
}

func (s *Store) List(ctx context.Context, workspaceID string, q cp.ListQuery) (*shared.PaginatedResult[cp.Row], error) {
	page := shared.NormalizePaginationWithin(q.Pagination(), cp.MaxPageSize)
	if !validUUIDs(workspaceID) || (q.HolderID != "" && !validUUIDs(q.HolderID)) {
		return shared.NewPaginatedResultWithin([]cp.Row{}, page, 0, cp.MaxPageSize), nil
	}
	scope, args := " WHERE p.workspace_id = ?", []interface{}{workspaceID}
	if q.HolderID != "" {
		scope, args = scope+" AND p.holder_id = ?", append(args, q.HolderID)
	}
	rows := "WITH rows AS (" + listRowsSQL + scope + ") SELECT * FROM rows"
	countSQL, countArgs := "SELECT COUNT(*) FROM customer_portfolio_entries p"+scope, args
	if search := strings.TrimSpace(q.Search); search != "" {
		rows += listSearchSQL
		args = append(args, sqlexpr.LikeContains(search))
		countSQL, countArgs = "WITH rows AS ("+listRowsSQL+scope+") SELECT COUNT(*) FROM rows"+listSearchSQL, args
	}
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
		return nil, err
	}
	var found []listRow
	if err := db.Raw(rows+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?", append(args, page.PageSize, page.OffsetWithin(cp.MaxPageSize))...).Scan(&found).Error; err != nil {
		return nil, err
	}
	items := make([]cp.Row, 0, len(found))
	for _, row := range found {
		items = append(items, row.toDomain())
	}
	return shared.NewPaginatedResultWithin(items, page, total, cp.MaxPageSize), nil
}
