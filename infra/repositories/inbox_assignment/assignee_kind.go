package inbox_assignment_repository

import "vozko/domain/actor"

// splitAssignee turns a domain assignee (a user id, ai:<agent id> or
// workflow:<workflow id>) into the storage pair: the bare uuid for the uuid
// column, and its kind.
func splitAssignee(assignee string) (string, string) {
	switch actor.KindOf(assignee) {
	case actor.KindAI:
		return actor.ParseAI(assignee), string(actor.KindAI)
	case actor.KindWorkflow:
		return actor.ParseWorkflow(assignee), string(actor.KindWorkflow)
	}
	return assignee, string(actor.KindHuman)
}

// joinAssignee is the inverse of splitAssignee. Anything that is not an
// automation kind, including rows written before the column existed, is a person.
func joinAssignee(id, kind string) string {
	switch actor.Kind(kind) {
	case actor.KindAI:
		return actor.FormatAI(id)
	case actor.KindWorkflow:
		return actor.FormatWorkflow(id)
	}
	return id
}
