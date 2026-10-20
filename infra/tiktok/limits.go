package tiktok

import "time"

type Family string

const (
	FamilyOAuth         Family = "oauth"
	FamilyWebhookConfig Family = "webhook_config"
	FamilyProfile       Family = "profile"
	FamilyVideos        Family = "videos"
	FamilyCommentList   Family = "comment_list"
	FamilyCommentReply  Family = "comment_reply"
	FamilyCommentHide   Family = "comment_hide"
	FamilyCommentLike   Family = "comment_like"
	FamilyMessageSend   Family = "message_send"
	FamilyMessageRead   Family = "message_read"
	FamilyMediaUpload   Family = "media_upload"
	FamilyMediaDownload Family = "media_download"
	FamilyCapabilities  Family = "capabilities"
)

const limiterWindow = time.Minute

const AccountsAPIPerMinute = 40

const DefaultAppPerMinute = 600

var familyBudgets = map[Family]int{
	FamilyOAuth:         300,
	FamilyWebhookConfig: 20,
	FamilyProfile:       AccountsAPIPerMinute,
	FamilyVideos:        AccountsAPIPerMinute,
	FamilyCommentList:   AccountsAPIPerMinute,
	FamilyCommentReply:  AccountsAPIPerMinute,
	FamilyCommentHide:   AccountsAPIPerMinute,
	FamilyCommentLike:   AccountsAPIPerMinute,
	FamilyMessageSend:   120,
	FamilyMessageRead:   120,
	FamilyMediaUpload:   60,
	FamilyMediaDownload: 120,
	FamilyCapabilities:  60,
}

func Families() []Family {
	return []Family{
		FamilyOAuth, FamilyWebhookConfig, FamilyProfile, FamilyVideos,
		FamilyCommentList, FamilyCommentReply, FamilyCommentHide, FamilyCommentLike,
		FamilyMessageSend, FamilyMessageRead, FamilyMediaUpload, FamilyMediaDownload, FamilyCapabilities,
	}
}

func (f Family) Budget() int { return familyBudgets[f] }

func (f Family) BackgroundBudget() int { return max(1, familyBudgets[f]*3/4) }

func (f Family) limiterPrefix() string { return "tt_" + string(f) }

func (f Family) backgroundPrefix() string { return "tt_bg_" + string(f) }

const appLimiterPrefix = "tt_app"

const appLimiterKey = "tt:app"

func accountFamilyKey(openID string, f Family) string { return "tt:" + openID + ":" + string(f) }

func appFamilyKey(f Family) string { return "tt:app:" + string(f) }
