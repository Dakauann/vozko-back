package attendance_repository

var ownerActorIDSQL = actorIDSQL("ia.assigned_user_id", "ia.assignee_kind")

func actorIDSQL(idColumn, kindColumn string) string {
	return `CASE ` + kindColumn + `
					WHEN 'ai' THEN 'ai:' || ` + idColumn + `::text
					WHEN 'workflow' THEN 'workflow:' || ` + idColumn + `::text
					ELSE COALESCE(` + idColumn + `::text, '')
				END`
}

func ownerLabelJoinsSQL(alias string) string {
	return `LEFT JOIN users u ON u.id::text = ` + alias + `.assigned_user_id
		LEFT JOIN agents ag ON 'ai:' || ag.id::text = ` + alias + `.assigned_user_id
		LEFT JOIN workflows wf ON 'workflow:' || wf.id::text = ` + alias + `.assigned_user_id`
}

func ownerLabelSQL(alias string) string {
	return `COALESCE(NULLIF(u.username, ''), NULLIF(u.email, ''), NULLIF(ag.name, ''), NULLIF(wf.name, ''), ` + alias + `.assigned_user_id)`
}

const ownerLabelGroupBy = `u.username, u.email, ag.name, wf.name`
