package agentturn

import "strings"

const (
	replyFeature      = "agent_reply"
	simulationFeature = "agent_simulation"
)

func ReplySession(subjectID string) string {
	return session(replyFeature, subjectID)
}

func SimulationSession(subjectID string) string {
	return session(simulationFeature, subjectID)
}

func session(feature, subjectID string) string {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ""
	}
	return feature + ":" + subjectID
}
