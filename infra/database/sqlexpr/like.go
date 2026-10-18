package sqlexpr

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func LikePrefix(prefix string) string {
	return likeEscaper.Replace(prefix) + "%"
}

func LikeContains(text string) string {
	return "%" + likeEscaper.Replace(text) + "%"
}
