package copilot

import (
	"errors"
	"testing"
)

func TestAProposalNeverCarriesAChoiceTheModelMade(t *testing.T) {
	choices := []ChoiceField{{Key: "image_model", Kind: ChoiceImageModel}}
	args := map[string]interface{}{"prompt": "pizza", "image_model": "x/invented"}
	stripped := StripChoices(args, choices)
	if _, kept := stripped["image_model"]; kept || stripped["prompt"] != "pizza" {
		t.Fatalf("stripped %v", stripped)
	}
	if args["image_model"] != "x/invented" {
		t.Fatal("the original arguments were changed")
	}
}

func TestTheUserChoiceReplacesWhateverTheArgumentsHeld(t *testing.T) {
	choices := []ChoiceField{{Key: "image_model", Kind: ChoiceImageModel}}
	args := map[string]interface{}{"prompt": "pizza", "image_model": "x/invented"}
	out, err := WithChoices(args, choices, map[string]string{"image_model": " openai/gpt-image-2 ", "other": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if out["image_model"] != "openai/gpt-image-2" || out["prompt"] != "pizza" {
		t.Fatalf("out %v", out)
	}
	if _, leaked := out["other"]; leaked {
		t.Fatal("a value no choice asked for reached the tool")
	}
}

func TestAnApprovalWithoutTheChoiceIsRefused(t *testing.T) {
	choices := []ChoiceField{{Key: "image_model", Kind: ChoiceImageModel}}
	for _, provided := range []map[string]string{nil, {"image_model": "  "}} {
		if _, err := WithChoices(map[string]interface{}{}, choices, provided); !errors.Is(err, ErrChoiceMissing) {
			t.Fatalf("%v: got %v", provided, err)
		}
	}
	out, err := WithChoices(map[string]interface{}{"a": 1, "image_model": "x"}, nil, map[string]string{"image_model": "y"})
	if err != nil || out["image_model"] != "x" {
		t.Fatalf("no choices asked: %v %v", out, err)
	}
}
