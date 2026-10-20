package mercadolivre

import "time"

type Family string

const (
	FamilyMessagesRead  Family = "messages:read"
	FamilyMessagesWrite Family = "messages:write"
	FamilyQuestions     Family = "questions"
	FamilyItems         Family = "items"
	FamilyOAuth         Family = "oauth"
	FamilyOther         Family = "other"
)

func Families() []Family {
	return []Family{FamilyMessagesRead, FamilyMessagesWrite, FamilyQuestions, FamilyItems, FamilyOAuth, FamilyOther}
}

const limiterWindow = time.Minute

const (
	MessagesReadPerMinute  = 400
	MessagesWritePerMinute = 400
	QuestionsPerMinute     = 300
	ItemsPerMinute         = 300
	OAuthPerMinute         = 100
	OtherPerMinute         = 300
	AccountPerMinute       = 120
)

var familyBudgets = map[Family]int{
	FamilyMessagesRead:  MessagesReadPerMinute,
	FamilyMessagesWrite: MessagesWritePerMinute,
	FamilyQuestions:     QuestionsPerMinute,
	FamilyItems:         ItemsPerMinute,
	FamilyOAuth:         OAuthPerMinute,
	FamilyOther:         OtherPerMinute,
}

const accountLimiterPrefix = "ml_account"

func (f Family) limiterPrefix() string { return "ml_app_" + string(f) }

func (f Family) Key() string { return "ml:app:" + string(f) }

func accountKey(userID string) string { return "ml:" + userID }

const BackgroundPerMinute = 150

const backgroundLimiterPrefix = "ml_app_background"

const BackgroundLimiterKey = "ml:app:background"
