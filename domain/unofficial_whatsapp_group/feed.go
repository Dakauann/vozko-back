package unofficial_whatsapp_group

import (
	"strings"

	"vozko/domain/unofficial_whatsapp"
)

func HandleJID(handle string) string {
	handle = strings.TrimSpace(handle)
	if phone := unofficial_whatsapp.NormalizePhone(handle); phone != "" && strings.HasPrefix(handle, "+") {
		return phone + "@" + unofficial_whatsapp.DomainUser
	}
	return handle
}

func HandleKey(handle string) string {
	jid := HandleJID(handle)
	return PersonKey(unofficial_whatsapp.PhoneFromJID(jid), jid)
}

type FeedContact struct {
	Name    string
	Picture string
}

type FeedNames struct {
	Members  map[string]string
	Contacts map[string]FeedContact
	Own      OwnNumbers
}

type FeedAuthor struct {
	Name    string
	Picture string
	OurTeam bool
}

func (n FeedNames) Author(outbound bool, handle string) FeedAuthor {
	if outbound {
		return FeedAuthor{Name: OurTeamAuthor, OurTeam: true}
	}
	handle = strings.TrimSpace(handle)
	key := HandleKey(handle)
	if key != "" && n.Own.Contains(key) {
		return FeedAuthor{Name: OurTeamAuthor, OurTeam: true}
	}
	contact := n.Contacts[handle]
	author := FeedAuthor{Picture: contact.Picture}
	switch {
	case key != "" && n.Members[key] != "":
		author.Name = n.Members[key]
	case contact.Name != "" && contact.Name != handle:
		author.Name = contact.Name
	case strings.HasPrefix(handle, "+"):
		author.Name = handle
	}
	return author
}
