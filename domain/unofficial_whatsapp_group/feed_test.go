package unofficial_whatsapp_group

import "testing"

func TestHandleJID(t *testing.T) {
	cases := []struct {
		handle string
		want   string
	}{
		{"+5511970001234", "5511970001234@s.whatsapp.net"},
		{" +5511970001234 ", "5511970001234@s.whatsapp.net"},
		{"5511970001234@s.whatsapp.net", "5511970001234@s.whatsapp.net"},
		{"123456789@lid", "123456789@lid"},
		{"", ""},
	}
	for _, c := range cases {
		if got := HandleJID(c.handle); got != c.want {
			t.Errorf("HandleJID(%q) = %q, want %q", c.handle, got, c.want)
		}
	}
}

func TestHandleKeyMatchesTheMemberPersonKey(t *testing.T) {
	cases := []struct {
		handle string
		want   string
	}{
		{"+5511970001234", "5511970001234"},
		{"5511970001234@s.whatsapp.net", "5511970001234"},
		{"123456789@lid", "123456789@lid"},
		{"", ""},
	}
	for _, c := range cases {
		if got := HandleKey(c.handle); got != c.want {
			t.Errorf("HandleKey(%q) = %q, want %q", c.handle, got, c.want)
		}
	}
}

func TestFeedAuthor(t *testing.T) {
	names := FeedNames{
		Members: map[string]string{"5511970001234": "Ana do grupo", "123456789@lid": "Oculto com nome"},
		Contacts: map[string]FeedContact{
			"+5511970001234": {Name: "Ana contato", Picture: "https://img/ana.jpg"},
			"+5511970005555": {Name: "Bruno contato", Picture: "https://img/bruno.jpg"},
			"+5511970006666": {Name: "+5511970006666"},
		},
		Own: NewOwnNumbers([]string{"5511970009999"}, nil),
	}
	cases := []struct {
		name     string
		outbound bool
		handle   string
		want     FeedAuthor
	}{
		{"our outbound message is our team", true, "operator-uuid", FeedAuthor{Name: OurTeamAuthor, OurTeam: true}},
		{"another of our numbers is our team", false, "+5511970009999", FeedAuthor{Name: OurTeamAuthor, OurTeam: true}},
		{"member name wins over the contact", false, "+5511970001234", FeedAuthor{Name: "Ana do grupo", Picture: "https://img/ana.jpg"}},
		{"contact name when not a member", false, "+5511970005555", FeedAuthor{Name: "Bruno contato", Picture: "https://img/bruno.jpg"}},
		{"a contact named by its own handle falls back to the phone", false, "+5511970006666", FeedAuthor{Name: "+5511970006666"}},
		{"unknown phone shows the phone", false, "+5511970007777", FeedAuthor{Name: "+5511970007777"}},
		{"hidden member by lid uses the member name", false, "123456789@lid", FeedAuthor{Name: "Oculto com nome"}},
		{"unknown lid has no name", false, "987654321@lid", FeedAuthor{}},
		{"empty handle has no name", false, "", FeedAuthor{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := names.Author(c.outbound, c.handle); got != c.want {
				t.Errorf("Author(%v, %q) = %+v, want %+v", c.outbound, c.handle, got, c.want)
			}
		})
	}
}

func TestFeedAuthorWithoutLookupsStillNamesOurTeam(t *testing.T) {
	var names FeedNames
	if got := names.Author(true, ""); got != (FeedAuthor{Name: OurTeamAuthor, OurTeam: true}) {
		t.Fatalf("Author = %+v, want our team", got)
	}
	if got := names.Author(false, "+5511970001234"); got != (FeedAuthor{Name: "+5511970001234"}) {
		t.Fatalf("Author = %+v, want the phone", got)
	}
}
