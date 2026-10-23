package emailcampaign_usecase

import (
	"strings"
	"testing"

	"vozko/domain/emaildesign"
)

func previewDocument() emaildesign.Document {
	return emaildesign.Document{Version: emaildesign.CurrentVersion, Blocks: []emaildesign.Block{
		{ID: "title", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Oi", Level: 1}},
		{ID: "footer", Kind: emaildesign.KindFooter, Footer: &emaildesign.Footer{CompanyName: "Loja", Address: "Rua A, 1"}},
	}}
}

func TestAPreviewCarriesBlockIDsOnlyWhenAnnotated(t *testing.T) {
	svc := &Service{previewURL: "https://app.example.com" + PreviewPath}
	plain, err := svc.Render(PreviewInput{Document: previewDocument()})
	if err != nil || strings.Contains(plain.HTML, "data-block-id") {
		t.Fatalf("a plain preview has no block ids, got %v", err)
	}
	annotated, err := svc.Render(PreviewInput{Document: previewDocument(), Annotate: true})
	if err != nil || !strings.Contains(annotated.HTML, `data-block-id="title"`) || !strings.Contains(annotated.HTML, `data-block-id="footer"`) {
		t.Fatalf("an annotated preview names every block, got %v", err)
	}
}

func TestADraftPreviewShowsAnEmailThatCannotBeSentYet(t *testing.T) {
	svc := &Service{previewURL: "https://app.example.com" + PreviewPath}
	doc := previewDocument()
	doc.Blocks = append([]emaildesign.Block{{ID: "cta", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Comprar"}}}, doc.Blocks...)
	if _, err := svc.Render(PreviewInput{Document: doc}); err == nil {
		t.Fatal("the regular preview refuses a document that cannot be sent")
	}
	if _, err := svc.Render(PreviewInput{Document: doc, Draft: true}); err != nil {
		t.Fatalf("a draft preview shows it, got %v", err)
	}
}
