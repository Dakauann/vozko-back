package sqlexpr

import "strings"

func column(alias, name string) string {
	if alias == "" {
		return name
	}
	return alias + "." + name
}

func Literal(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func LiteralList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = Literal(v)
	}
	return strings.Join(quoted, ", ")
}

func stringsOf[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
