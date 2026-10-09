package lead

import "vozko/domain/recordevent"

func FieldChange(kind recordevent.Kind, actorID, field string, before, after any, recorded bool) recordevent.Event {
	event := recordevent.Event{Actor: actorID, Kind: kind, Changes: recordevent.Diff(fieldRecord(field, before), fieldRecord(field, after))}
	return event.Redact(func(string) bool { return !recorded })
}

func fieldRecord(field string, value any) map[string]any {
	if value == nil {
		return nil
	}
	return map[string]any{field: value}
}
