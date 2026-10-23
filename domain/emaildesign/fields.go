package emaildesign

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

const DefaultSpacerHeight = 24

const (
	fieldText    = "text"
	fieldRatio   = "ratio"
	fieldPadding = "padding"
	fieldBack    = "background_color"
)

type FieldError struct {
	Code  string
	Field string
}

func (e *FieldError) Error() string {
	return "emaildesign: " + e.Code + " (" + e.Field + ")"
}

func fieldFailure(code, field string) error {
	return &FieldError{Code: code, Field: field}
}

func (b Block) Fields() map[string]interface{} {
	out := map[string]interface{}{"id": b.ID, "kind": string(b.Kind)}
	if b.Padding != "" {
		out[fieldPadding] = string(b.Padding)
	}
	if b.BackgroundColor != "" {
		out[fieldBack] = b.BackgroundColor
	}
	switch {
	case b.Kind == KindText && b.Text != nil:
		out[fieldText] = FormatInline(b.Text.Paragraphs)
		if b.Text.Align != "" {
			out["align"] = string(b.Text.Align)
		}
	case b.Kind == KindColumns && b.Columns != nil:
		out[fieldRatio] = string(b.Columns.Ratio)
		columns := make([][]map[string]interface{}, 0, len(b.Columns.Columns))
		for _, column := range b.Columns.Columns {
			children := make([]map[string]interface{}, 0, len(column.Blocks))
			for _, child := range column.Blocks {
				children = append(children, child.Fields())
			}
			columns = append(columns, children)
		}
		out["columns"] = columns
	default:
		if props := b.props(); props != nil {
			for key, value := range jsonObject(props) {
				out[key] = value
			}
		}
	}
	return out
}

func jsonObject(value interface{}) map[string]interface{} {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func (b Block) props() interface{} {
	switch b.Kind {
	case KindHeading:
		return b.Heading
	case KindText:
		return b.Text
	case KindButton:
		return b.Button
	case KindImage:
		return b.Image
	case KindDivider:
		return b.Divider
	case KindSpacer:
		return b.Spacer
	case KindColumns:
		return b.Columns
	case KindSocial:
		return b.Social
	case KindFooter:
		return b.Footer
	}
	return nil
}

func (b *Block) setProps(kind BlockKind, raw map[string]json.RawMessage) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	decode := func(target interface{}) error {
		dec := json.NewDecoder(bytes.NewReader(encoded))
		dec.DisallowUnknownFields()
		return dec.Decode(target)
	}
	switch kind {
	case KindHeading:
		b.Heading = &Heading{}
		return decode(b.Heading)
	case KindText:
		b.Text = &Text{}
		return decode(b.Text)
	case KindButton:
		b.Button = &Button{}
		return decode(b.Button)
	case KindImage:
		b.Image = &Image{}
		return decode(b.Image)
	case KindDivider:
		b.Divider = &Divider{}
		return decode(b.Divider)
	case KindSpacer:
		b.Spacer = &Spacer{}
		return decode(b.Spacer)
	case KindColumns:
		b.Columns = &Columns{}
		return decode(b.Columns)
	case KindSocial:
		b.Social = &Social{}
		return decode(b.Social)
	case KindFooter:
		b.Footer = &Footer{}
		return decode(b.Footer)
	}
	return fmt.Errorf("%w: %q", ErrUnknownBlockKind, kind)
}

var propTypes = map[BlockKind]reflect.Type{
	KindHeading: reflect.TypeOf(Heading{}),
	KindText:    reflect.TypeOf(Text{}),
	KindButton:  reflect.TypeOf(Button{}),
	KindImage:   reflect.TypeOf(Image{}),
	KindDivider: reflect.TypeOf(Divider{}),
	KindSpacer:  reflect.TypeOf(Spacer{}),
	KindColumns: reflect.TypeOf(Columns{}),
	KindSocial:  reflect.TypeOf(Social{}),
	KindFooter:  reflect.TypeOf(Footer{}),
}

func jsonKeys(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for _, f := range reflect.VisibleFields(t) {
		tag := f.Tag.Get("json")
		if name, _, _ := cutComma(tag); name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

func cutComma(tag string) (string, string, bool) {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i], tag[i+1:], true
		}
	}
	return tag, "", false
}

func KindFields(kind BlockKind) []string {
	t, ok := propTypes[kind]
	if !ok {
		return nil
	}
	keys := []string{fieldPadding, fieldBack}
	for _, key := range jsonKeys(t) {
		switch {
		case kind == KindText && key == "paragraphs":
			keys = append(keys, fieldText)
		case kind == KindColumns && key == "columns":
		default:
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func SettingsFields() []string {
	keys := jsonKeys(reflect.TypeOf(Settings{}))
	sort.Strings(keys)
	return keys
}

func allowed(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func sortedKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func kindDefaults(kind BlockKind) (Block, bool) {
	switch kind {
	case KindHeading:
		return Block{Heading: &Heading{Level: 2}}, true
	case KindText:
		return Block{Text: &Text{}}, true
	case KindButton:
		return Block{Button: &Button{}}, true
	case KindImage:
		return Block{Image: &Image{Width: MaxImageWidth}}, true
	case KindDivider:
		return Block{Divider: &Divider{Color: defaultDividerColor, Thickness: MinDividerThickness}}, true
	case KindSpacer:
		return Block{Spacer: &Spacer{Height: DefaultSpacerHeight}}, true
	case KindColumns:
		return Block{Columns: &Columns{Ratio: Ratio5050, Columns: []Column{{Blocks: []Block{}}, {Blocks: []Block{}}}}}, true
	case KindSocial:
		return Block{Social: &Social{}}, true
	case KindFooter:
		return Block{Footer: &Footer{}}, true
	}
	return Block{}, false
}

func NewBlock(id string, kind BlockKind, fields map[string]json.RawMessage) (Block, error) {
	block, ok := kindDefaults(kind)
	if !ok {
		return Block{}, fmt.Errorf("%w: %q", ErrUnknownBlockKind, kind)
	}
	block.ID = id
	block.Kind = kind
	return PatchBlock(block, fields)
}

func PatchBlock(b Block, fields map[string]json.RawMessage) (Block, error) {
	keys := KindFields(b.Kind)
	current := b.props()
	if keys == nil || current == nil {
		return Block{}, fmt.Errorf("%w: %q", ErrUnknownBlockKind, b.Kind)
	}
	props := map[string]json.RawMessage{}
	encoded, err := json.Marshal(current)
	if err != nil {
		return Block{}, err
	}
	if err := json.Unmarshal(encoded, &props); err != nil {
		return Block{}, err
	}
	out := Block{ID: b.ID, Kind: b.Kind, Padding: b.Padding, BackgroundColor: b.BackgroundColor}
	var ratio *ColumnRatio
	for _, key := range sortedKeys(fields) {
		value := fields[key]
		if !allowed(keys, key) {
			return Block{}, fieldFailure("field_not_for_kind", key)
		}
		switch {
		case key == fieldPadding:
			if json.Unmarshal(value, &out.Padding) != nil {
				return Block{}, fieldFailure("field_invalid", key)
			}
		case key == fieldBack:
			if json.Unmarshal(value, &out.BackgroundColor) != nil {
				return Block{}, fieldFailure("field_invalid", key)
			}
		case b.Kind == KindText && key == fieldText:
			paragraphs, err := markupParagraphs(value)
			if err != nil {
				return Block{}, err
			}
			props["paragraphs"] = paragraphs
		case b.Kind == KindColumns && key == fieldRatio:
			var next ColumnRatio
			if json.Unmarshal(value, &next) != nil || next.shares() == nil {
				return Block{}, fieldFailure("field_invalid", key)
			}
			ratio = &next
		default:
			props[key] = value
		}
	}
	if err := out.setProps(b.Kind, props); err != nil {
		return Block{}, decodeFailure(err)
	}
	if ratio != nil {
		out.Columns = redistributed(*out.Columns, *ratio)
	}
	return out, nil
}

func markupParagraphs(value json.RawMessage) (json.RawMessage, error) {
	var markup string
	if json.Unmarshal(value, &markup) != nil {
		return nil, fieldFailure("field_invalid", fieldText)
	}
	paragraphs, err := ParseInline(markup)
	if err != nil {
		return nil, fieldFailure("text_markup_invalid", fieldText)
	}
	return json.Marshal(paragraphs)
}

func decodeFailure(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fieldFailure("field_invalid", typeErr.Field)
	}
	var kindErr *FieldError
	if errors.As(err, &kindErr) {
		return err
	}
	return fieldFailure("field_invalid", "")
}

func redistributed(c Columns, ratio ColumnRatio) *Columns {
	count := len(ratio.shares())
	columns := make([]Column, 0, count)
	for i := 0; i < count && i < len(c.Columns); i++ {
		columns = append(columns, Column{Blocks: append([]Block{}, c.Columns[i].Blocks...)})
	}
	for i := count; i < len(c.Columns); i++ {
		last := &columns[count-1]
		last.Blocks = append(last.Blocks, c.Columns[i].Blocks...)
	}
	for len(columns) < count {
		columns = append(columns, Column{Blocks: []Block{}})
	}
	return &Columns{Ratio: ratio, Columns: columns}
}

func PatchSettings(s Settings, fields map[string]json.RawMessage) (Settings, error) {
	keys := SettingsFields()
	props := map[string]json.RawMessage{}
	encoded, err := json.Marshal(s)
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(encoded, &props); err != nil {
		return Settings{}, err
	}
	for _, key := range sortedKeys(fields) {
		if !allowed(keys, key) {
			return Settings{}, fieldFailure("field_not_for_kind", key)
		}
		props[key] = fields[key]
	}
	merged, err := json.Marshal(props)
	if err != nil {
		return Settings{}, err
	}
	var out Settings
	dec := json.NewDecoder(bytes.NewReader(merged))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Settings{}, decodeFailure(err)
	}
	return out, nil
}
