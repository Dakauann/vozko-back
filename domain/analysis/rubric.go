package analysis

import (
	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// TRANSITIONAL SHIM. See analysis.go.
//
// The rubric that used to live here is now
// domain/comment_analysis/conversation.go, next to the comment rubric it will
// sit beside in the unified engine. Nothing is redefined here: every name below
// is an alias or a forwarding call, so there is exactly one copy of the
// taxonomy and one copy of the quality weights.
//
// That single copy is the whole point. These weights were previously restated
// in the domain, the tool schema and two prompt strings, and had already
// diverged: one prompt scored 35/25/25/15 while the schema and the other prompt
// used 40/30/20/10, for the same 0-100 number.

type (
	ClassificationField  = shared.ClassificationField
	ClassificationOption = shared.ClassificationOption
	QualityLevel         = shared.QualityLevel
	QualityDimension     = shared.QualityDimension
	QualityAssessment    = ca.ConversationQuality
)

const (
	QualityLevelNone   = shared.QualityLevelNone
	QualityLevelLow    = shared.QualityLevelLow
	QualityLevelMedium = shared.QualityLevelMedium
	QualityLevelHigh   = shared.QualityLevelHigh

	QualityKeyGoalProgress       = ca.QualityKeyGoalProgress
	QualityKeyCustomerEngagement = ca.QualityKeyCustomerEngagement
	QualityKeyAgentConduct       = ca.QualityKeyAgentConduct
	QualityKeyProfessionalism    = ca.QualityKeyProfessionalism
)

func QualityLevelValues() []string { return shared.QualityLevelValues() }

func ClassificationFields() []ClassificationField { return ca.ConversationClassificationFields() }

func ClassificationRubricPrompt() string { return ca.ConversationRubricPrompt() }

func QualityDimensions() []QualityDimension { return ca.ConversationQualityDimensions() }

func NewQualityAssessment(levels map[string]QualityLevel) QualityAssessment {
	return ca.NewConversationQuality(levels)
}

func QualityRubricPrompt() string { return ca.ConversationQualityRubricPrompt() }
