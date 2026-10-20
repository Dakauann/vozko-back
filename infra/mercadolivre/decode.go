package mercadolivre

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999-0700",
	"2006-01-02T15:04:05-0700",
}

func parseTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("mercadolivre: unreadable time %q", raw)
}

type flexTime struct {
	time.Time
	Set bool
}

func (t *flexTime) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*t = flexTime{}
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if strings.TrimSpace(s) == "" {
		*t = flexTime{}
		return nil
	}
	parsed, err := parseTime(s)
	if err != nil {
		return err
	}
	*t = flexTime{Time: parsed, Set: true}
	return nil
}

func (t flexTime) ptr() *time.Time {
	if !t.Set {
		return nil
	}
	v := t.Time
	return &v
}

type flexText string

func (f *flexText) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*f = ""
		return nil
	}
	if len(raw) > 0 && raw[0] == '{' {
		var obj struct {
			Plain string `json:"plain"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return err
		}
		*f = flexText(obj.Plain)
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	*f = flexText(s)
	return nil
}

type flexIDs []string

func (f *flexIDs) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*f = nil
		return nil
	}
	if len(raw) > 0 && raw[0] == '[' {
		var items []mldomain.FlexibleID
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		out := make([]string, 0, len(items))
		for _, id := range items {
			if id != "" {
				out = append(out, id.String())
			}
		}
		*f = out
		return nil
	}
	var single mldomain.FlexibleID
	if err := json.Unmarshal(raw, &single); err != nil {
		return err
	}
	if single == "" {
		*f = nil
		return nil
	}
	*f = []string{single.String()}
	return nil
}

type userRef struct {
	UserID mldomain.FlexibleID `json:"user_id"`
}

type idRef struct {
	ID mldomain.FlexibleID `json:"id"`
}

type paging struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func boundLimit(limit, ceiling int) int {
	if limit <= 0 || limit > ceiling {
		return ceiling
	}
	return limit
}
