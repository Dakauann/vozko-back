package whatsappform_repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
	"vozko/infra/repositories/departmentscope"
)

var errIDsInvalid = errors.New("whatsappform repository: ids must be uuids")

var matchedResults = sqlexpr.LiteralList([]string{string(whatsappform.MatchByToken), string(whatsappform.MatchByContext)})

func joinsFor(alias string) string {
	return " LEFT JOIN whatsapp_campaign_entries wce ON wce.id = " + alias + ".entry_id AND " + alias + ".entry_type = " + sqlexpr.Literal(string(shared.EntryTypeWhatsApp)) +
		" LEFT JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = " + alias + ".workspace_id"
}

func scopeClause(scope whatsappform.ReadScope) (string, []interface{}, error) {
	if !scope.Resolved {
		return "", nil, whatsappform.ErrForbidden
	}
	clause, args := departmentscope.Clause("wc.department_id", "wce.id", scope.DepartmentIDs, scope.Restrict, scope.AssigneeUserID)
	return clause, args, nil
}

type conditions struct {
	sql  strings.Builder
	args []interface{}
}

func where(sql string, args ...interface{}) *conditions {
	c := &conditions{}
	return c.and(sql, args...)
}

func (c *conditions) and(sql string, args ...interface{}) *conditions {
	c.sql.WriteString(sql)
	c.args = append(c.args, args...)
	return c
}

func (c *conditions) String() string {
	return c.sql.String()
}

func (c *conditions) within(column string, from, to *time.Time) *conditions {
	if from != nil {
		c.and(" AND "+column+" >= ?", *from)
	}
	if to != nil {
		c.and(" AND "+column+" <= ?", *to)
	}
	return c
}

func pageSize(limit int) (int, error) {
	switch {
	case limit == 0:
		return whatsappform.DefaultPageSize, nil
	case limit < 0 || limit > whatsappform.MaxPageSize:
		return 0, whatsappform.ErrListQueryInvalid
	}
	return limit, nil
}

func cursorOf(after string) (*shared.Keyset, error) {
	key, paged, err := shared.ParseKeyset(after)
	if err != nil || (paged && !database.AllUUIDs(key.ID)) {
		return nil, whatsappform.ErrListQueryInvalid
	}
	if !paged {
		return nil, nil
	}
	return &key, nil
}

func pageOf[T any](items []T, limit int, key func(T) shared.Keyset) whatsappform.Page[T] {
	if len(items) <= limit {
		return whatsappform.Page[T]{Items: items}
	}
	items = items[:limit]
	return whatsappform.Page[T]{Items: items, Next: key(items[limit-1]).Encode()}
}

func jsonText(value interface{}, empty string) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return empty, nil
	}
	return string(raw), nil
}

func rawJSON(raw json.RawMessage) schema.OptionalText {
	if len(raw) == 0 {
		return ""
	}
	return schema.OptionalText(raw)
}

func decodeJSON(text string, into interface{}) error {
	if text == "" || text == "null" {
		return nil
	}
	return json.Unmarshal([]byte(text), into)
}
