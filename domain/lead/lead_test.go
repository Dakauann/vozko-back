package lead

import "testing"

func TestNormalizeNumber(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"valid 13-digit mobile", "5511987654321", "5511987654321"},
		{"valid 12-digit old mobile", "551187654321", "551187654321"},
		{"strips leading +", "+5511987654321", ""},
		{"strips spaces", " 5511987654321 ", "5511987654321"},
		{"too short", "551198765", ""},
		{"too long", "55119876543210", ""},
		{"not BR prefix", "1415551234567", ""},
		{"empty string", "", ""},
		{"letters", "abcdefg", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeNumber(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeNumber(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeRawNumber(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		// SIP From headers: national number without country code → prepend 55.
		{"inbound mobile no country code", "84994409624", "5584994409624"},
		{"inbound landline no country code", "8433334444", "558433334444"},
		{"already 55-prefixed 13", "5584994409624", "5584994409624"},
		{"already 55-prefixed 12", "558433334444", "558433334444"},
		{"strips + and spaces", "+55 84 99440-9624", "5584994409624"},
		{"too short", "994409624", ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeRawNumber(tc.input); got != tc.want {
				t.Errorf("NormalizeRawNumber(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeWhatsAppNumber(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{

		{
			"13-digit mobile already normalised",
			"5511987654321",
			"5511987654321",
		},
		{
			"13-digit mobile DDD 21 (RJ)",
			"5521987654321",
			"5521987654321",
		},

		{
			"12-digit legacy mobile first digit 9",
			"5511987654321"[:13],
			"5511987654321",
		},
		{
			"12-digit legacy mobile first digit 6 (DDD 11)",
			"551162345678",
			"5511962345678",
		},
		{
			"12-digit legacy mobile first digit 7",
			"551172345678",
			"5511972345678",
		},
		{
			"12-digit legacy mobile first digit 8",
			"551182345678",
			"5511982345678",
		},

		{

			"12-digit landline first digit 3 (Meta 131026 regression)",
			"551134567890",
			"551134567890",
		},
		{
			"12-digit landline first digit 2",
			"551123456789",
			"551123456789",
		},
		{
			"12-digit corporate first digit 4",
			"551143216789",
			"551143216789",
		},
		{
			"12-digit landline DDD 31 (BH) first digit 3",
			"553133445566",
			"553133445566",
		},
		{

			"12-digit SP landline first digit 5 must NOT be modified",
			"551158888784",
			"551158888784",
		},
		{
			"12-digit SP landline first digit 5 DDD 21 must NOT be modified",
			"552158888784",
			"552158888784",
		},

		{
			"10-digit local mobile no country code",
			"11987654321",
			"5511987654321",
		},
		{
			"11-digit with 9 no country code",
			"11962345678",
			"5511962345678",
		},

		{
			"completely invalid returns input unchanged",
			"not-a-number",
			"not-a-number",
		},
		{
			"empty returns empty",
			"",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeWhatsAppNumber(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeWhatsAppNumber(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestGetAlternatePhoneFormat(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{

		{
			"13-digit mobile → 12-digit",
			"5511987654321",
			"551187654321",
		},
		{
			"13-digit mobile DDD 21",
			"5521987654321",
			"552187654321",
		},

		{
			"12-digit mobile first digit 6 → 13-digit",
			"551162345678",
			"5511962345678",
		},
		{
			"12-digit mobile first digit 8 → 13-digit",
			"551182345678",
			"5511982345678",
		},

		{
			"12-digit landline first digit 3 → no alternate",
			"551134567890",
			"",
		},
		{
			"12-digit landline first digit 2 → no alternate",
			"551123456789",
			"",
		},
		{
			"12-digit corporate first digit 4 → no alternate",
			"551143216789",
			"",
		},
		{

			"12-digit SP landline first digit 5 → no alternate",
			"551158888784",
			"",
		},

		{
			"invalid number returns empty",
			"not-a-number",
			"",
		},
		{
			"empty returns empty",
			"",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetAlternatePhoneFormat(tc.input)
			if got != tc.want {
				t.Errorf("GetAlternatePhoneFormat(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLeadValidate(t *testing.T) {
	age := 25
	negAge := -1
	cases := []struct {
		name string
		l    Lead
		want error
	}{
		{"missing workspace", Lead{Number: "5511987654321"}, ErrLeadWorkspaceRequired},
		{"missing number", Lead{WorkspaceID: "ws-1"}, ErrLeadRequired},
		{"invalid number", Lead{WorkspaceID: "ws-1", Number: "abc"}, ErrLeadInvalid},
		{"negative age", Lead{WorkspaceID: "ws-1", Number: "5511987654321", Age: &negAge}, ErrLeadInvalid},
		{"valid", Lead{WorkspaceID: "ws-1", Number: "5511987654321", Age: &age}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.l.Validate(); got != tc.want {
				t.Errorf("Validate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLeadNormalize(t *testing.T) {
	zero := 0
	l := Lead{ID: "  id-1 ", WorkspaceID: "  ws-1 ", Number: " 5511987654321 ", Name: "  Jane ", Age: &zero}
	l.Normalize()
	if l.ID != "id-1" || l.WorkspaceID != "ws-1" || l.Number != "5511987654321" || l.Name != "Jane" {
		t.Errorf("Normalize result unexpected: %+v", l)
	}
	if l.Age != nil {
		t.Error("expected Age cleared for non-positive value")
	}
}

func TestLeadMerge(t *testing.T) {
	age := 33
	l := Lead{Name: "Old", ProfilePictureURL: "old.png"}
	l.Merge(LeadUpdate{Name: "New", ProfilePictureURL: "new.png", Age: &age})
	if l.Name != "New" || l.ProfilePictureURL != "new.png" || l.Age == nil || *l.Age != 33 {
		t.Errorf("Merge result unexpected: %+v", l)
	}

	l.Merge(LeadUpdate{})
	if l.Name != "New" || l.ProfilePictureURL != "new.png" || *l.Age != 33 {
		t.Errorf("empty Merge mutated values: %+v", l)
	}
}
