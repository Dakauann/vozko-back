package workflow

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	customerTrue          = "Sim"
	customerFalse         = "Não"
	customerDateLayout    = "02/01/2006"
	customerStampLayout   = "02/01/2006 15:04"
	isoDateLayout         = "2006-01-02"
	customerListSeparator = ", "
)

var isoDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func RenderCustomerText(text string, state *RunState) (string, []string) {
	var unresolved []string
	rendered := interpolation{
		valueText:    CustomerValueText,
		blankMissing: true,
		unresolved:   func(ref string) { unresolved = append(unresolved, ref) },
	}.apply(text, state)
	return rendered, unresolved
}

func CustomerText(ctx *NodeContext, text string) string {
	rendered, unresolved := RenderCustomerText(text, ctx.State)
	ctx.Trace.RecordUnresolved(unresolved...)
	return rendered
}

func CustomerValueText(val interface{}) string {
	switch v := val.(type) {
	case nil:
		return ""
	case bool:
		if v {
			return customerTrue
		}
		return customerFalse
	case string:
		return customerString(v)
	case float64:
		return customerNumber(v)
	case float32:
		return customerNumber(float64(v))
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return customerNumber(f)
		}
		return v.String()
	case []interface{}:
		return customerList(v)
	}
	return TemplateText(val)
}

func customerString(s string) string {
	if isoDatePattern.MatchString(s) {
		if day, err := time.Parse(isoDateLayout, s); err == nil {
			return day.Format(customerDateLayout)
		}
	}
	if len(s) > len(isoDateLayout) && isoDatePattern.MatchString(s[:len(isoDateLayout)]) {
		if stamp, err := time.Parse(time.RFC3339, s); err == nil {
			return stamp.Format(customerStampLayout)
		}
	}
	return s
}

func customerNumber(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return TemplateText(f)
	}
	return strings.Replace(strconv.FormatFloat(f, 'f', -1, 64), ".", ",", 1)
}

func customerList(items []interface{}) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch item.(type) {
		case map[string]interface{}, []interface{}:
			return TemplateText(items)
		}
		parts = append(parts, CustomerValueText(item))
	}
	return strings.Join(parts, customerListSeparator)
}
