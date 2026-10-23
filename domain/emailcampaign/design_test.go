package emailcampaign

import (
	"errors"
	"testing"

	"vozko/domain/emaildesign"
)

func TestDesignInputValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      DesignInput
		wantErr error
	}{
		{name: "valid", in: DesignInput{Name: "Boas-vindas", Kind: DesignTemplate, Document: readyDocument()}},
		{name: "with thumbnail", in: DesignInput{Name: "x", Kind: DesignCampaign, Document: readyDocument(), ThumbnailURL: "https://cdn.example.com/t.png"}},
		{name: "no name", in: DesignInput{Kind: DesignTemplate, Document: readyDocument()}, wantErr: ErrDesignNameRequired},
		{name: "bad kind", in: DesignInput{Name: "x", Kind: "draft", Document: readyDocument()}, wantErr: ErrDesignKindInvalid},
		{name: "plain http thumbnail", in: DesignInput{Name: "x", Kind: DesignTemplate, Document: readyDocument(), ThumbnailURL: "http://cdn.example.com/t.png"}, wantErr: ErrThumbnailInvalid},
		{name: "invalid document", in: DesignInput{Name: "x", Kind: DesignTemplate}, wantErr: emaildesign.ErrInvalidDocument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestADesignWithDraftGapsCannotBeSaved(t *testing.T) {
	doc := readyDocument()
	doc.Blocks = append([]emaildesign.Block{{ID: "cta", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Comprar"}}}, doc.Blocks...)
	err := DesignInput{Name: "Black Friday", Kind: DesignTemplate, Document: doc}.Validate()
	var invalid *emaildesign.ValidationError
	if !errors.As(err, &invalid) || !errors.Is(err, emaildesign.ErrInvalidDocument) || invalid.Issues[0].Code != "href_missing" {
		t.Fatalf("a draft gap still blocks saving with its coded issue, got %v", err)
	}
}
