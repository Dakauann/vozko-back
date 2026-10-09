package lead

import (
	"reflect"
	"testing"
)

func TestMergeIncomingNamePriority(t *testing.T) {
	cases := []struct {
		name         string
		stored       string
		storedSource Source
		incoming     string
		source       Source
		wantName     string
		wantSource   Source
		wantChanged  []string
	}{
		{"a channel name fills an empty name", "", "", "Ana", SourceChannel, "Ana", SourceChannel, []string{FieldName}},
		{"a channel name never replaces a channel name", "Ana", SourceChannel, "Aninha", SourceChannel, "Ana", SourceChannel, nil},
		{"a channel name never replaces an imported name", "Ana Souza", SourceImport, "Aninha", SourceChannel, "Ana Souza", SourceImport, nil},
		{"a channel name never replaces a manual name", "Ana Souza", SourceManual, "Aninha", SourceChannel, "Ana Souza", SourceManual, nil},
		{"an import fills an empty name", "", "", "Ana Souza", SourceImport, "Ana Souza", SourceImport, []string{FieldName}},
		{"an import replaces a channel name", "Aninha", SourceChannel, "Ana Souza", SourceImport, "Ana Souza", SourceImport, []string{FieldName}},
		{"an import never replaces another import", "Ana Souza", SourceImport, "Ana S.", SourceImport, "Ana Souza", SourceImport, nil},
		{"an import never replaces a manual name", "Ana Souza", SourceManual, "Ana S.", SourceImport, "Ana Souza", SourceManual, nil},
		{"a name of unknown origin is kept like an imported one", "Ana Souza", "", "Aninha", SourceImport, "Ana Souza", "", nil},
		{"a manual name replaces an imported one", "Ana S.", SourceImport, "Ana Souza", SourceManual, "Ana Souza", SourceManual, []string{FieldName}},
		{"a stored name that is the number counts as no name", "5511987654321", "", "Ana", SourceChannel, "Ana", SourceChannel, []string{FieldName}},
		{"an incoming name that is the number is ignored", "", "", "+55 (11) 98765-4321", SourceChannel, "", "", nil},
		{"an incoming name in the other ninth digit format is ignored", "", "", "551187654321", SourceImport, "", "", nil},
		{"an empty incoming name leaves the name alone", "Ana", SourceChannel, "   ", SourceImport, "Ana", SourceChannel, nil},
		{"whitespace is collapsed before comparing", "Ana Souza", SourceChannel, "  Ana   Souza ", SourceImport, "Ana Souza", SourceChannel, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := Lead{Number: "5511987654321", Name: tc.stored, NameSource: tc.storedSource}
			changed := l.MergeIncoming(LeadUpdate{Source: tc.source, Name: tc.incoming})
			if l.Name != tc.wantName || l.NameSource != tc.wantSource {
				t.Fatalf("name = %q (%q), want %q (%q)", l.Name, l.NameSource, tc.wantName, tc.wantSource)
			}
			if !reflect.DeepEqual(changed, tc.wantChanged) {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
		})
	}
}

func TestMergeIncomingIgnoresANameThatCannotBeStored(t *testing.T) {
	l := Lead{Number: "5511987654321"}
	long := make([]rune, MaxLeadNameLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if changed := l.MergeIncoming(LeadUpdate{Source: SourceChannel, Name: string(long)}); changed != nil || l.Name != "" {
		t.Fatalf("an oversized name must not be stored, got %q (%v)", l.Name, changed)
	}
}

func TestMergeIncomingKeepsTheStoredAgeAndNeverWritesOne(t *testing.T) {
	age := 30
	kept := Lead{Number: "5511987654321", StoredAge: &age}
	if changed := kept.MergeIncoming(LeadUpdate{Source: SourceImport, Name: "Maria"}); !reflect.DeepEqual(changed, []string{FieldName}) || *kept.StoredAge != 30 {
		t.Fatalf("changed = %v, age = %v", changed, kept.StoredAge)
	}
	empty := Lead{Number: "5511987654322"}
	if empty.MergeIncoming(LeadUpdate{Source: SourceImport, Name: "Bruna"}); empty.StoredAge != nil {
		t.Fatalf("an age was written: %v", *empty.StoredAge)
	}
}

func TestMergeIncomingRefreshesTheProfilePicture(t *testing.T) {
	l := Lead{Number: "5511987654321", ProfilePictureURL: "old.png"}
	if changed := l.MergeIncoming(LeadUpdate{Source: SourceChannel, ProfilePictureURL: "new.png"}); !reflect.DeepEqual(changed, []string{FieldProfilePicture}) || l.ProfilePictureURL != "new.png" {
		t.Fatalf("picture = %q (%v)", l.ProfilePictureURL, changed)
	}
	if changed := l.MergeIncoming(LeadUpdate{Source: SourceChannel}); changed != nil || l.ProfilePictureURL != "new.png" {
		t.Fatalf("an empty picture must leave the stored one, got %q", l.ProfilePictureURL)
	}
}

func TestMergeIncomingNeverTouchesBlockOwnerOrConsent(t *testing.T) {
	l := Lead{Number: "5511987654321", Blocked: true, Owner: "u-1"}
	l.MergeIncoming(LeadUpdate{Source: SourceManual, Name: "Ana"})
	if !l.Blocked || l.Owner != "u-1" {
		t.Fatalf("incoming data changed state it does not own: %+v", l)
	}
}

func TestRealName(t *testing.T) {
	cases := []struct {
		name   string
		number string
		stored string
		want   string
	}{
		{"an ordinary name", "5511987654321", "Ana Souza", "Ana Souza"},
		{"no name", "5511987654321", "", ""},
		{"the identity number", "5511987654321", "5511987654321", ""},
		{"the identity number formatted", "5511987654321", "+55 11 98765-4321", ""},
		{"the identity in the other ninth digit format", "5511987654321", "551187654321", ""},
		{"a different number is still a name", "5511987654321", "5521912345678", "5521912345678"},
		{"a name that carries the number is still a name", "5511987654321", "Ana 11987654321", "Ana 11987654321"},
		{"a lead without identity keeps its name", "", "Ana", "Ana"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := Lead{Number: tc.number, Name: tc.stored}
			if got := l.RealName(); got != tc.want {
				t.Fatalf("RealName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name string
		lead Lead
		want string
	}{
		{"a real name", Lead{Number: "5511987654321", Name: "Ana"}, "Ana"},
		{"a mobile number", Lead{Number: "5511987654321"}, "+55 11 98765-4321"},
		{"a landline number", Lead{Number: "551133334444"}, "+55 11 3333-4444"},
		{"a name that is the number", Lead{Number: "5511987654321", Name: "5511987654321"}, "+55 11 98765-4321"},
		{"no name and no number", Lead{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lead.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName = %q, want %q", got, tc.want)
			}
		})
	}
}
