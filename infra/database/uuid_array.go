package database

import (
	"database/sql/driver"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
)

func UUIDArray(ids []string) pq.StringArray {
	seen := make(map[string]struct{}, len(ids))
	out := make(pq.StringArray, 0, len(ids))
	for _, raw := range ids {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		id := parsed.String()
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func AllUUIDs(ids ...string) bool {
	for _, id := range ids {
		if len(UUIDArray([]string{id})) == 0 {
			return false
		}
	}
	return true
}

type UUIDs []uuid.UUID

func UUIDsOf(ids []string) UUIDs {
	valid := UUIDArray(ids)
	out := make(UUIDs, len(valid))
	for i, id := range valid {
		out[i] = uuid.MustParse(id)
	}
	return out
}

func (u UUIDs) Strings() []string {
	out := make([]string, len(u))
	for i, id := range u {
		out[i] = id.String()
	}
	return out
}

func (u UUIDs) Value() (driver.Value, error) {
	return pq.StringArray(u.Strings()).Value()
}

func (u UUIDs) Dimensions() []pgtype.ArrayDimension {
	if u == nil {
		return nil
	}
	return []pgtype.ArrayDimension{{Length: int32(len(u)), LowerBound: 1}}
}

func (u UUIDs) Index(i int) any {
	return [16]byte(u[i])
}

func (u UUIDs) IndexType() any {
	return [16]byte{}
}

type Texts []string

func (t Texts) Value() (driver.Value, error) {
	return pq.StringArray(t).Value()
}

func (t Texts) Dimensions() []pgtype.ArrayDimension {
	return pgtype.FlatArray[string](t).Dimensions()
}

func (t Texts) Index(i int) any {
	return t[i]
}

func (t Texts) IndexType() any {
	return ""
}
