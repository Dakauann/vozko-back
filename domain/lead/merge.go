package lead

import (
	"strings"

	"vozko/domain/shared"
)

const (
	FieldNumber         = "number"
	FieldName           = "name"
	FieldNickname       = "nickname"
	FieldEmail          = "email"
	FieldBirthDate      = "birthDate"
	FieldOwner          = "owner"
	FieldWhatsAppOptIn  = "whatsappOptIn"
	FieldOptedOut       = "optedOut"
	FieldOptOutSource   = "optOutSource"
	FieldBlocked        = "blocked"
	FieldProfilePicture = "profilePictureUrl"
	FieldPhones         = "phones"
	FieldAddresses      = "addresses"
	FieldRelations      = "relations"
	FieldCustomFields   = "customFields"
	FieldAnonymized     = "anonymized"
)

func (l *Lead) MergeIncoming(update LeadUpdate) []string {
	var changed []string
	if l.mergeName(update.Source, update.Name) {
		changed = append(changed, FieldName)
	}
	if picture := strings.TrimSpace(update.ProfilePictureURL); picture != "" && picture != l.ProfilePictureURL {
		l.ProfilePictureURL = picture
		changed = append(changed, FieldProfilePicture)
	}
	return changed
}

func (l *Lead) mergeName(source Source, raw string) bool {
	incoming := NormalizeName(raw)
	if incoming == "" || ValidateName(incoming) != nil || l.isOwnNumber(incoming) {
		return false
	}
	current := l.RealName()
	if current == incoming {
		return false
	}
	if current != "" && nameRank(source) <= nameRank(l.NameSource) {
		return false
	}
	l.Name = incoming
	l.NameSource = source
	return true
}

func nameRank(source Source) int {
	switch source {
	case SourceManual:
		return 3
	case SourceChannel:
		return 1
	}
	return 2
}

func (l *Lead) RealName() string {
	name := NormalizeName(l.Name)
	if name == "" || l.isOwnNumber(name) {
		return ""
	}
	return name
}

func (l *Lead) SplitName() (string, string) {
	first, rest, _ := strings.Cut(l.RealName(), " ")
	return first, rest
}

func (l *Lead) DisplayName() string {
	if name := l.RealName(); name != "" {
		return name
	}
	return FormatNumber(l.Number)
}

func (l *Lead) isOwnNumber(name string) bool {
	if !l.HasIdentity() || !looksLikeAPhone(name) {
		return false
	}
	parsed, err := shared.ParsePhone(name)
	if err != nil {
		return false
	}
	for _, format := range NumberFormats(l.Number) {
		if format == parsed {
			return true
		}
	}
	return false
}

func looksLikeAPhone(name string) bool {
	return strings.Trim(name, "0123456789+-() .") == ""
}

func FormatNumber(number string) string {
	canonical := NormalizeNumber(number)
	if canonical == "" {
		return strings.TrimSpace(number)
	}
	area, subscriber := canonical[2:4], canonical[4:]
	split := len(subscriber) - 4
	return "+55 " + area + " " + subscriber[:split] + "-" + subscriber[split:]
}
