package shared

import "testing"

func TestEnsureDialablePhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "keeps mobile already with ninth digit",
			input:    "5511998765432",
			expected: "5511998765432",
		},
		{
			name:     "adds ninth digit to legacy mobile",
			input:    "551188765432",
			expected: "5511988765432",
		},
		{
			name:     "formats fixed line as 0+DDD+number",
			input:    "551123456789",
			expected: "01123456789",
		},
		{
			name:     "formats fixed line prefix 3 as 0+DDD+number",
			input:    "551133762000",
			expected: "01133762000",
		},
		{
			name:     "strips 55 from landline already with 0 prefix",
			input:    "5501133762000",
			expected: "01133762000",
		},
		{
			name:     "keeps rural fixed line prefix 57 as 0+DDD",
			input:    "553157123456",
			expected: "03157123456",
		},
		{
			name:     "supports formatted brazil number",
			input:    "+55 (11) 8876-5432",
			expected: "5511988765432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := EnsureDialablePhoneNumber(tt.input)
			if actual != tt.expected {
				t.Fatalf("EnsureDialablePhoneNumber(%q) = %q, expected %q", tt.input, actual, tt.expected)
			}
		})
	}
}
