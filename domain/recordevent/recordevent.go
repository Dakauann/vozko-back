package recordevent

import (
	"reflect"
	"sort"
)

type Kind string

type Change struct {
	Field    string `json:"field"`
	Before   any    `json:"before"`
	After    any    `json:"after"`
	Redacted bool   `json:"redacted,omitempty"`
}

type Event struct {
	Actor   string
	Kind    Kind
	Changes []Change
}

func (e Event) Empty() bool {
	return len(e.Changes) == 0
}

func (e Event) Fields() []string {
	return FieldsOf([]Event{e})
}

func FieldsOf(events []Event) []string {
	seen := map[string]struct{}{}
	var fields []string
	for _, e := range events {
		for _, c := range e.Changes {
			if _, dup := seen[c.Field]; dup {
				continue
			}
			seen[c.Field] = struct{}{}
			fields = append(fields, c.Field)
		}
	}
	sort.Strings(fields)
	return fields
}

func Diff(before, after map[string]any) []Change {
	keys := map[string]struct{}{}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	fields := make([]string, 0, len(keys))
	for k := range keys {
		fields = append(fields, k)
	}
	sort.Strings(fields)

	var changes []Change
	for _, field := range fields {
		was, had := before[field]
		now, has := after[field]
		if had == has && reflect.DeepEqual(was, now) {
			continue
		}
		changes = append(changes, Change{Field: field, Before: was, After: now})
	}
	return changes
}

func (e Event) Redact(hidden func(field string) bool) Event {
	if e.Changes == nil {
		return e
	}
	changes := make([]Change, len(e.Changes))
	for i, c := range e.Changes {
		if hidden(c.Field) {
			c = Change{Field: c.Field, Redacted: true}
		}
		changes[i] = c
	}
	e.Changes = changes
	return e
}
