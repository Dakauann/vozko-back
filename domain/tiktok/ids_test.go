package tiktok

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidNumericID(t *testing.T) {
	cases := map[string]bool{
		"7247303576418566913":   true,
		"1":                     true,
		"":                      false,
		"72473035764185669131":  true,
		"724730357641856691312": false,
		"-1":                    false,
		"12a":                   false,
		" 12":                   false,
	}
	for id, want := range cases {
		if got := ValidNumericID(id); got != want {
			t.Errorf("ValidNumericID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestValidOpaqueIDAcceptsTikTokShapesAndRefusesUnsafeOnes(t *testing.T) {
	accepted := []string{
		"_000abPL1cJIdVefgDhGijkl2mFn3AB0abY4",
		"a81e31c6-3b57-40d7-9253-c2f211e70514",
		"+ABc1D2/E0fGhijklMNOrD34vDW/zDiZUNRH5Jyahue6OcA7b8Zp==",
		"S:1:abc+def/ghi=",
	}
	for _, id := range accepted {
		if !ValidOpaqueID(id) {
			t.Errorf("ValidOpaqueID(%q) = false, want true", id)
		}
	}
	refused := []string{"", " ", "abc def", "abc\n", "abc\x00", "<script>", strings.Repeat("a", MaxOpaqueIDLength+1), "abc\"", "a,b"}
	for _, id := range refused {
		if ValidOpaqueID(id) {
			t.Errorf("ValidOpaqueID(%q) = true, want false", id)
		}
	}
}

func TestNumericIDKeepsEveryDigitOfLargeJSONNumbers(t *testing.T) {
	var payload struct {
		CommentID NumericID `json:"comment_id"`
		VideoID   NumericID `json:"video_id"`
		Parent    NumericID `json:"parent_comment_id"`
	}
	raw := `{"comment_id":7247303576418566913,"video_id":"7203946942097902849","parent_comment_id":null}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.CommentID != "7247303576418566913" {
		t.Errorf("comment id = %q, lost precision", payload.CommentID)
	}
	if payload.VideoID != "7203946942097902849" {
		t.Errorf("video id = %q", payload.VideoID)
	}
	if payload.Parent != "" {
		t.Errorf("null parent = %q, want empty", payload.Parent)
	}
}

func TestNumericIDRefusesNonIntegers(t *testing.T) {
	for _, raw := range []string{`1.5`, `-3`, `"abc"`, `true`, `1e10`, `{}`} {
		var id NumericID
		if err := json.Unmarshal([]byte(raw), &id); err == nil {
			t.Errorf("NumericID accepted %s as %q", raw, id)
		}
	}
}
