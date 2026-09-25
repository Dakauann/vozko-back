package attendance_repository

// ownerActorIDSQL turns a conversation's inbox_assignments row into the actor
// id the rest of the product uses: the user id for a person, ai:<id> for an
// agent, workflow:<id> for a workflow. The table keeps the bare uuid plus
// assignee_kind, so reading assigned_user_id alone would pass an agent off as
// a person.
var ownerActorIDSQL = actorIDSQL("ia.assigned_user_id", "ia.assignee_kind")

func actorIDSQL(idColumn, kindColumn string) string {
	return `CASE ` + kindColumn + `
					WHEN 'ai' THEN 'ai:' || ` + idColumn + `::text
					WHEN 'workflow' THEN 'workflow:' || ` + idColumn + `::text
					ELSE COALESCE(` + idColumn + `::text, '')
				END`
}

// ownerLabelJoinsSQL joins whatever an owner actor id can name: a user, an
// agent or a workflow. alias is the table carrying assigned_user_id.
func ownerLabelJoinsSQL(alias string) string {
	return `LEFT JOIN users u ON u.id::text = ` + alias + `.assigned_user_id
		LEFT JOIN agents ag ON 'ai:' || ag.id::text = ` + alias + `.assigned_user_id
		LEFT JOIN workflows wf ON 'workflow:' || wf.id::text = ` + alias + `.assigned_user_id`
}

// ownerLabelSQL names an owner, falling back to its id only when none of the
// joins in ownerLabelJoinsSQL found it.
func ownerLabelSQL(alias string) string {
	return `COALESCE(NULLIF(u.username, ''), NULLIF(u.email, ''), NULLIF(ag.name, ''), NULLIF(wf.name, ''), ` + alias + `.assigned_user_id)`
}

// ownerLabelGroupBy lists the label's columns for a GROUP BY.
const ownerLabelGroupBy = `u.username, u.email, ag.name, wf.name`
