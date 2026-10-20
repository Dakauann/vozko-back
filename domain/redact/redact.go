package redact

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const Mask = "••••"

const documentDigitMask = "•"

var sensitiveKeyParts = []string{
	"password",
	"senha",
	"secret",
	"token",
	"authorization",
	"cookie",
	"apikey",
	"credential",
}

var (
	embeddedPairPattern = regexp.MustCompile(`"((?:[^"\\]|\\.){1,128})"(\s*:\s*)("(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?)`)
	assignmentPattern   = regexp.MustCompile(`([A-Za-z0-9_.\-]{1,128})=([^&\s"'<>]+)`)
	bearerPattern       = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9\-._~+/]+=*`)
	digitRunPattern     = regexp.MustCompile(`\d[\d./-]*\d`)
	formattedCPF        = regexp.MustCompile(`^\d{3}\.\d{3}\.\d{3}-\d{2}$`)
	formattedCNPJ       = regexp.MustCompile(`^\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}$`)
)

type Redactor struct {
	secrets   *strings.Replacer
	sensitive bool
}

func New(secretValues ...string) Redactor {
	return Redactor{secrets: secretReplacer(secretValues), sensitive: true}
}

func SecretsOnly(secretValues ...string) Redactor {
	return Redactor{secrets: secretReplacer(secretValues)}
}

func IsSensitiveKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	if normalized == "" {
		return false
	}
	counterless := strings.ReplaceAll(normalized, tokenCounterPart, "")
	for _, part := range sensitiveKeyParts {
		if strings.Contains(counterless, part) {
			return true
		}
	}
	return false
}

const tokenCounterPart = "tokens"

func (r Redactor) Map(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if r.sensitive && IsSensitiveKey(k) {
			out[k] = maskedValue(v)
			continue
		}
		out[k] = r.Value(v)
	}
	return out
}

func (r Redactor) Value(v interface{}) interface{} {
	switch typed := v.(type) {
	case string:
		return r.String(typed)
	case map[string]interface{}:
		return r.Map(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = r.Value(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, item := range typed {
			if r.sensitive && IsSensitiveKey(k) && item != "" {
				out[k] = Mask
				continue
			}
			out[k] = r.String(item)
		}
		return out
	case []string:
		return r.Strings(typed)
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(typed))
		for i, item := range typed {
			out[i] = r.Map(item)
		}
		return out
	default:
		return v
	}
}

func (r Redactor) Strings(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, item := range values {
		out[i] = r.String(item)
	}
	return out
}

func (r Redactor) String(s string) string {
	if s == "" {
		return s
	}
	if r.secrets != nil {
		s = r.secrets.Replace(s)
	}
	if !r.sensitive {
		return s
	}
	s = maskEmbeddedPairs(s)
	s = maskAssignments(s)
	s = bearerPattern.ReplaceAllString(s, "${1} "+Mask)
	return maskDocuments(s)
}

func maskedValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok && s == "" {
		return s
	}
	return Mask
}

func secretReplacer(values []string) *strings.Replacer {
	seen := make(map[string]bool)
	forms := make([]string, 0, len(values)*4)
	for _, v := range values {
		if v == "" {
			continue
		}
		for _, form := range secretForms(v) {
			if form != "" && !seen[form] {
				seen[form] = true
				forms = append(forms, form)
			}
		}
	}
	if len(forms) == 0 {
		return nil
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	pairs := make([]string, 0, len(forms)*2)
	for _, form := range forms {
		pairs = append(pairs, form, Mask)
	}
	return strings.NewReplacer(pairs...)
}

func secretForms(v string) []string {
	forms := []string{v, url.QueryEscape(v), url.PathEscape(v)}
	if encoded, err := json.Marshal(v); err == nil && len(encoded) >= 2 {
		forms = append(forms, string(encoded[1:len(encoded)-1]))
	}
	return forms
}

func maskEmbeddedPairs(s string) string {
	if !strings.Contains(s, `"`) {
		return s
	}
	return embeddedPairPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := embeddedPairPattern.FindStringSubmatch(match)
		if !IsSensitiveKey(parts[1]) {
			return match
		}
		return `"` + parts[1] + `"` + parts[2] + `"` + Mask + `"`
	})
}

func maskAssignments(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	return assignmentPattern.ReplaceAllStringFunc(s, func(match string) string {
		key, _, _ := strings.Cut(match, "=")
		if !IsSensitiveKey(key) {
			return match
		}
		return key + "=" + Mask
	})
}

func maskDocuments(s string) string {
	return digitRunPattern.ReplaceAllStringFunc(s, func(run string) string {
		if !isDocument(run) {
			return run
		}
		return maskAllButLastTwoDigits(run)
	})
}

func isDocument(run string) bool {
	if formattedCPF.MatchString(run) || formattedCNPJ.MatchString(run) {
		return true
	}
	if !onlyDigits(run) {
		return false
	}
	switch len(run) {
	case 11:
		return validCPF(run)
	case 14:
		return validCNPJ(run)
	}
	return false
}

func maskAllButLastTwoDigits(run string) string {
	digits := 0
	for _, c := range run {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	var b strings.Builder
	seen := 0
	for _, c := range run {
		if c < '0' || c > '9' {
			b.WriteRune(c)
			continue
		}
		seen++
		if seen > digits-2 {
			b.WriteRune(c)
			continue
		}
		b.WriteString(documentDigitMask)
	}
	return b.String()
}

func onlyDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func validCPF(d string) bool {
	return d[9] == checkDigit(d[:9], []int{10, 9, 8, 7, 6, 5, 4, 3, 2}) &&
		d[10] == checkDigit(d[:10], []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2})
}

func validCNPJ(d string) bool {
	return d[12] == checkDigit(d[:12], []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) &&
		d[13] == checkDigit(d[:13], []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2})
}

func checkDigit(digits string, weights []int) byte {
	sum := 0
	for i, w := range weights {
		sum += int(digits[i]-'0') * w
	}
	rest := sum % 11
	if rest < 2 {
		return '0'
	}
	return byte('0' + 11 - rest)
}
