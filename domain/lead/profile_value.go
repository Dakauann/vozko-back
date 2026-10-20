package lead

import "vozko/domain/customfield"

func (l *Lead) ProfileValue(field string) (interface{}, bool) {
	if key, ok := customFieldKey(field); ok {
		value, found := l.CustomFields[key]
		if !found {
			return nil, false
		}
		return customfield.FormatValue(value), true
	}
	switch field {
	case FieldEmail:
		return l.Email, l.Email != ""
	case FieldBirthDate:
		if l.BirthDate == nil {
			return nil, false
		}
		return l.BirthDate.String(), true
	case FieldAddresses:
		if primary := l.PrimaryAddress(); primary != nil {
			return primary.Postal, true
		}
	}
	return nil, false
}
