package lead

import (
	"context"
	"fmt"
	"strings"
)

type EmailFacts struct {
	ID           string
	Name         string
	Email        string
	Company      string
	CustomFields map[string]any
	OptedOut     bool
	Blocked      bool
}

func (f EmailFacts) FirstName() string {
	l := Lead{Name: f.Name}
	first, _ := l.SplitName()
	return first
}

func (f EmailFacts) FullName() string {
	l := Lead{Name: f.Name}
	return l.RealName()
}

func (f EmailFacts) FieldValues() map[string]string {
	out := make(map[string]string, len(f.CustomFields))
	for key, value := range f.CustomFields {
		switch v := value.(type) {
		case nil:
		case string:
			if strings.TrimSpace(v) != "" {
				out[key] = v
			}
		case bool, float64, float32, int, int64, int32:
			out[key] = fmt.Sprint(v)
		}
	}
	return out
}

type EmailFactsReader interface {
	EmailFacts(ctx context.Context, workspaceID string, leadIDs []string) (map[string]EmailFacts, error)
}
