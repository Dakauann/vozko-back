package attendance_repository

import "strings"

type sqlQuery struct {
	parts []string
	args  []interface{}
}

func newSQLQuery() *sqlQuery {
	return &sqlQuery{}
}

func (q *sqlQuery) add(text string, args ...interface{}) *sqlQuery {
	q.parts = append(q.parts, text)
	q.args = append(q.args, args...)
	return q
}

func (q *sqlQuery) addQuery(other *sqlQuery) *sqlQuery {
	if other == nil {
		return q
	}
	q.parts = append(q.parts, strings.Join(other.parts, ""))
	q.args = append(q.args, other.args...)
	return q
}

func (q *sqlQuery) empty() bool {
	return q == nil || len(q.parts) == 0
}

func (q *sqlQuery) build() (string, []interface{}) {
	if q == nil {
		return "", nil
	}
	return strings.Join(q.parts, ""), q.args
}

func joinQueries(sep string, queries []*sqlQuery) *sqlQuery {
	out := newSQLQuery()
	for i, query := range queries {
		if i > 0 {
			out.add(sep)
		}
		out.addQuery(query)
	}
	return out
}
