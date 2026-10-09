package customfield

import (
	"slices"
	"strings"

	"vozko/domain/shared"
)

func (d *Definition) CoerceLocal(raw string) (any, error) {
	if d == nil {
		return nil, ErrUnknownKey
	}
	raw = strings.TrimSpace(raw)
	var value any
	switch d.Type {
	case TypeNumber:
		number, err := shared.ParseDecimal(raw)
		if err != nil {
			return nil, ErrValueType
		}
		value = number
	case TypeDate:
		date, err := shared.ParseLocalDate(raw)
		if err != nil {
			return nil, ErrValueType
		}
		value = date.String()
	case TypeBoolean:
		switch shared.FoldForMatch(raw) {
		case "sim", "s", "yes", "y", "1", "true", "verdadeiro", "x":
			value = true
		case "nao", "n", "no", "0", "false", "falso":
			value = false
		default:
			return nil, ErrValueType
		}
	default:
		coerced, err := d.Coerce(raw)
		if err != nil {
			return nil, err
		}
		value = coerced
	}
	if err := d.ValidateValue(value); err != nil {
		return nil, err
	}
	return value, nil
}

func MissingRequired(defs []*Definition, viewer Viewer, values map[string]any) []string {
	var missing []string
	for _, def := range visibleDefinitions(defs, viewer) {
		if def == nil || FormatValue(values[def.Key]) != "" {
			continue
		}
		if def.ValidateValue(nil) != nil {
			missing = append(missing, def.Key)
		}
	}
	slices.Sort(missing)
	return missing
}
