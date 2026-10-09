package geocoding

import "testing"

func TestNextTurnIsTheLastWorkspaceServedInRotatedOrder(t *testing.T) {
	served := func(workspaces ...string) []Claim {
		claims := make([]Claim, 0, len(workspaces))
		for _, ws := range workspaces {
			claims = append(claims, Claim{WorkspaceID: ws})
		}
		return claims
	}
	tests := []struct {
		name   string
		after  string
		claims []Claim
		want   string
	}{
		{"the first batch ends at the highest workspace it served", "", served("w1", "w3", "w2", "w3"), "w3"},
		{"a batch that did not wrap ends at its highest workspace", "w2", served("w3", "w5", "w4"), "w5"},
		{"a batch that wrapped ends at the highest workspace before the cursor", "w5", served("w6", "w7", "w1", "w2"), "w2"},
		{"a batch that wrapped onto the cursor ends there", "w5", served("w6", "w1", "w5"), "w5"},
		{"an empty batch keeps the cursor", "w4", nil, "w4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextTurn(tt.after, tt.claims); got != tt.want {
				t.Fatalf("NextTurn() = %q, want %q", got, tt.want)
			}
		})
	}
}
