package googlesheet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MaxCellRunes = 50000
	MaxRowBytes  = 1 << 20
	cellOverhead = 16
)

type Cells map[string]any

type SystemValues map[Source]string

var localDateTimeLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2/1/2006 15:04:05",
	"2/1/2006 15:04",
}

func (s *Sheet) Accept(values map[string]any, system SystemValues, now time.Time) (Cells, error) {
	if err := s.Writable(); err != nil {
		return nil, err
	}
	for key := range values {
		col, ok := s.Column(key)
		switch {
		case !ok:
			return nil, &ValueError{Column: key, Header: key, Reason: ErrUnknownColumn}
		case col.Archived:
			return nil, &ValueError{Column: key, Header: col.Header, Reason: ErrColumnArchived}
		case col.Source.IsSystem():
			return nil, &ValueError{Column: key, Header: col.Header, Reason: ErrSystemColumn}
		}
	}
	cells := Cells{}
	size := 0
	for _, col := range s.Columns {
		if col.Archived {
			continue
		}
		raw, present := values[col.Key]
		if col.Source.IsSystem() {
			raw, present = s.systemValue(col.Source, system, now)
		}
		value, err := s.normalize(col, raw, present)
		if err != nil {
			return nil, err
		}
		if value == nil {
			continue
		}
		cells[col.Key] = value
		size += cellSize(value)
	}
	if size > MaxRowBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrRowTooLarge, size)
	}
	return cells, nil
}

func (s *Sheet) systemValue(source Source, system SystemValues, now time.Time) (any, bool) {
	if source == SourceWrittenAt {
		return now.In(s.Location()).Format(time.RFC3339), true
	}
	value, ok := system[source]
	return value, ok && strings.TrimSpace(value) != ""
}

func (s *Sheet) normalize(col Column, raw any, present bool) (any, error) {
	refuse := func(reason error, detail string) error {
		return &ValueError{Column: col.Key, Header: col.Header, Reason: reason, Detail: detail}
	}
	if !present || raw == nil || isBlank(raw) {
		if col.Required {
			return nil, refuse(ErrRequiredColumnMissing, "")
		}
		return nil, nil
	}
	value, err := s.coerce(col, raw)
	switch {
	case err == nil:
		return value, nil
	case errors.Is(err, ErrValueNotInOptions):
		return nil, refuse(ErrValueNotInOptions, "opções: "+strings.Join(col.Options, ", "))
	case errors.Is(err, ErrValueTooLong):
		return nil, refuse(ErrValueTooLong, fmt.Sprintf("até %d caracteres", MaxCellRunes))
	}
	return nil, refuse(ErrInvalidValue, err.Error())
}

func isBlank(raw any) bool {
	text, ok := raw.(string)
	return ok && strings.TrimSpace(text) == ""
}

func (s *Sheet) coerce(col Column, raw any) (any, error) {
	switch col.Type {
	case TypeText, TypeLongText:
		return coerceText(raw)
	case TypeNumber:
		return coerceNumber(raw, false)
	case TypeMoney:
		return coerceNumber(raw, true)
	case TypeDate:
		text, err := scalarText(raw)
		if err != nil {
			return nil, err
		}
		date, err := shared.ParseLocalDate(text)
		if err != nil {
			return nil, fmt.Errorf("data %q não reconhecida; use AAAA-MM-DD ou DD/MM/AAAA", text)
		}
		return date.String(), nil
	case TypeDateTime:
		text, err := scalarText(raw)
		if err != nil {
			return nil, err
		}
		at, err := s.parseDateTime(text)
		if err != nil {
			return nil, err
		}
		return at.Format(time.RFC3339), nil
	case TypeBoolean:
		if b, ok := raw.(bool); ok {
			return b, nil
		}
		text, err := scalarText(raw)
		if err != nil {
			return nil, err
		}
		answer, err := shared.ParseYesNo(text)
		if err != nil {
			return nil, fmt.Errorf("use sim ou não, recebido %q", text)
		}
		return answer, nil
	case TypeChoice:
		text, err := scalarText(raw)
		if err != nil {
			return nil, err
		}
		folded := shared.FoldForMatch(text)
		for _, option := range col.Options {
			if shared.FoldForMatch(option) == folded {
				return option, nil
			}
		}
		return nil, ErrValueNotInOptions
	case TypeEmail:
		return coerceEmail(raw)
	case TypePhone:
		return coercePhone(raw)
	case TypeURL:
		return coerceURL(raw)
	}
	return nil, fmt.Errorf("tipo %q desconhecido", col.Type)
}

func scalarText(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case bool:
		return strconv.FormatBool(v), nil
	}
	return "", fmt.Errorf("valor do tipo %T não cabe em uma célula", raw)
}

func coerceText(raw any) (any, error) {
	text, err := scalarText(raw)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(text) > MaxCellRunes {
		return nil, ErrValueTooLong
	}
	return text, nil
}

var currencyMarks = strings.NewReplacer("R$", "", "US$", "", "$", "", "€", "", " ", "", " ", "")

func coerceNumber(raw any, money bool) (any, error) {
	switch v := raw.(type) {
	case float64:
		return finite(v)
	case float32:
		return finite(float64(v))
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	}
	text, err := scalarText(raw)
	if err != nil {
		return nil, err
	}
	if money {
		text = currencyMarks.Replace(text)
	}
	number, err := shared.ParseDecimal(text)
	if err != nil {
		return nil, fmt.Errorf("número %q não reconhecido", text)
	}
	return number, nil
}

func finite(v float64) (any, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, errors.New("número infinito")
	}
	return v, nil
}

func (s *Sheet) parseDateTime(text string) (time.Time, error) {
	loc := s.Location()
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at.In(loc), nil
	}
	for _, layout := range localDateTimeLayouts {
		if at, err := time.ParseInLocation(layout, text, loc); err == nil {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("data e hora %q não reconhecida; use AAAA-MM-DD HH:MM", text)
}

func coerceEmail(raw any) (any, error) {
	text, err := scalarText(raw)
	if err != nil {
		return nil, err
	}
	address, err := mail.ParseAddress(text)
	if err != nil || address.Name != "" || address.Address != text || !strings.Contains(text[strings.LastIndex(text, "@")+1:], ".") {
		return nil, fmt.Errorf("email %q inválido", text)
	}
	return strings.ToLower(text), nil
}

func coercePhone(raw any) (any, error) {
	text, err := scalarText(raw)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(text, "+") && !strings.HasPrefix(text, "+55") {
		digits := shared.DigitsOf(text)
		if len(digits) < 8 || len(digits) > 15 {
			return nil, fmt.Errorf("telefone %q inválido", text)
		}
		return "+" + digits, nil
	}
	number, err := shared.ParsePhone(text)
	if err != nil {
		return nil, fmt.Errorf("telefone %q inválido; use DDD e número ou +código do país", text)
	}
	return "+" + number, nil
}

func coerceURL(raw any) (any, error) {
	text, err := scalarText(raw)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(text)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, fmt.Errorf("link %q inválido; use http ou https", text)
	}
	if utf8.RuneCountInString(text) > MaxCellRunes {
		return nil, ErrValueTooLong
	}
	return text, nil
}

func cellSize(value any) int {
	if text, ok := value.(string); ok {
		return len(text) + cellOverhead
	}
	return cellOverhead * 2
}
