package lead

import (
	"errors"
	"strings"

	"vozko/domain/customfield"
)

const customFieldPrefix = FieldCustomFields + "."

var ErrCustomFieldsUnchecked = errors.New("lead: custom field values go through SetCustomFields, which checks them against the workspace's lead fields")

func CustomFieldName(key string) string {
	return customFieldPrefix + key
}

func customFieldKey(field string) (string, bool) {
	return strings.CutPrefix(field, customFieldPrefix)
}

func (l *Lead) SetCustomFields(patch map[string]any, defs []*customfield.Definition, viewer customfield.Viewer) error {
	next, err := customfield.ApplyValues(defs, viewer, l.CustomFields, patch)
	if err != nil {
		return err
	}
	l.CustomFields = next
	return nil
}

func (l *Lead) customFieldRecords() map[string]any {
	fields := make(map[string]any, len(l.CustomFields))
	for key, value := range l.CustomFields {
		fields[CustomFieldName(key)] = value
	}
	return fields
}

func unrecordable(defs []*customfield.Definition) func(field string) bool {
	return func(field string) bool {
		key, custom := customFieldKey(field)
		return custom && !customfield.Recordable(defs, key)
	}
}
