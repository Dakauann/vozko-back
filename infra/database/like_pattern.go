package database

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func EscapeLike(text string) string {
	return likeEscaper.Replace(text)
}

func LikePrefix(prefix string) string {
	return EscapeLike(prefix) + "%"
}

func LikeContains(text string) string {
	return "%" + EscapeLike(text) + "%"
}
