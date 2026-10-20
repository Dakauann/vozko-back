package sqlexpr

import "vozko/domain/workflow"

func WorkflowRunOrigin(alias string) string {
	startedBy := column(alias, "started_by")
	startedWith := func(origin string) string {
		return " WHEN " + startedBy + " LIKE " + Literal(workflow.RunOriginPrefix(origin)+"%") + " THEN " + Literal(origin)
	}
	return "(CASE" + startedWith(workflow.RunOriginLeadExit) + startedWith(workflow.RunOriginManual) +
		" ELSE CAST(" + column(alias, "trigger_type") + " AS text) END)"
}
