package metaerror

const (
	AccessRemoved     = 10
	Undeliverable     = 131026
	AccountLocked     = 131031
	PaymentProblem    = 131042
	SpamLimit         = 131048
	EngagementLimit   = 131049
	MarketingOptOut   = 131050
	ExperimentHoldout = 130472
	TemplatePaused    = 132015
	TemplateDisabled  = 132016
)

func AboutContact(code int) bool {
	return code == MarketingOptOut || code == ExperimentHoldout
}

const firstVozkoCode = 900000

func FromMeta(code int) bool {
	return code > 0 && code < firstVozkoCode
}
