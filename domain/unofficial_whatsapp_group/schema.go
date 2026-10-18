package unofficial_whatsapp_group

func ResponseSchema(fields []TrackedField) map[string]any {
	str := map[string]any{"type": "string"}
	strings := map[string]any{"type": "array", "items": str}
	runSentiments := []string{string(SentimentPositive), string(SentimentNegative), string(SentimentNeutral), string(SentimentMixed)}
	mentionSentiments := []string{"positive", "negative", "neutral"}
	alerts := []string{string(AlertLow), string(AlertMedium), string(AlertHigh)}

	object := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}
	}

	mention := object(map[string]any{
		"entity":    str,
		"sentiment": map[string]any{"type": "string", "enum": mentionSentiments},
		"frequency": map[string]any{"type": "integer", "minimum": 0},
		"context":   str,
	}, "entity", "sentiment", "frequency", "context")

	participant := object(map[string]any{
		"name":      str,
		"jid":       str,
		"sentiment": map[string]any{"type": "string", "enum": runSentiments},
		"messages":  map[string]any{"type": "integer", "minimum": 1},
		"summary":   str,
		"key_quote": str,
	}, "name", "jid", "sentiment", "messages", "summary", "key_quote")

	action := object(map[string]any{"situation": str, "strategy": str}, "situation", "strategy")

	return object(map[string]any{
		"summary":                str,
		"sentiment":              map[string]any{"type": "string", "enum": runSentiments},
		"key_themes":             strings,
		"relevant_mentions":      map[string]any{"type": "array", "items": mention},
		"participant_sentiments": map[string]any{"type": "array", "items": participant},
		"insights":               strings,
		"suggested_actions":      map[string]any{"type": "array", "items": action},
		"alert_level":            map[string]any{"type": "string", "enum": alerts},
		"custom_fields":          customFieldsSchema(fields),
	}, "summary", "sentiment", "key_themes", "relevant_mentions", "participant_sentiments",
		"insights", "suggested_actions", "alert_level", "custom_fields")
}

func customFieldsSchema(fields []TrackedField) map[string]any {
	props := make(map[string]any, len(fields))
	required := make([]string, 0, len(fields))
	for _, f := range fields {
		props[f.Key] = map[string]any{"type": []string{jsonTypeOf(f.Type), "null"}}
		required = append(required, f.Key)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}
}

func jsonTypeOf(t FieldType) string {
	switch t {
	case FieldNumber, FieldPercentage:
		return "number"
	case FieldBoolean:
		return "boolean"
	}
	return "string"
}
