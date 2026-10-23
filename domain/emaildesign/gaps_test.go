package emaildesign

import (
	"reflect"
	"strings"
	"testing"
)

func TestOpenGapsNameEachBlockThatStillBlocksSending(t *testing.T) {
	cta := buttonBlock("cta", "Agendar consulta", "")
	inside := buttonBlock("inner", "Ver mais", "")
	doc := docWith(cta, columnsBlock("cols", Ratio5050, []Block{inside}, nil))
	doc.Blocks[len(doc.Blocks)-1].Footer.CompanyName = ""
	doc.Settings.TextColor = "azul"
	got := OpenGaps(doc)
	want := []Gap{
		{Code: "color_invalid", Field: "settings.text_color"},
		{BlockID: "cta", Kind: KindButton, Text: "Agendar consulta", Code: "href_missing", Field: "href"},
		{BlockID: "inner", Kind: KindButton, Text: "Ver mais", Code: "href_missing", Field: "href"},
		{BlockID: "footer", Kind: KindFooter, Code: "footer_company_missing", Field: "company_name"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if len(OpenGaps(fullDocumentWithoutGaps())) != 0 {
		t.Fatal("a document that can be sent has no gaps")
	}
}

func TestAGapQuotesAShortExcerptOfTheBlock(t *testing.T) {
	long := Block{ID: "t", Kind: KindHeading, Heading: &Heading{Text: strings.Repeat("palavra ", 20), Level: 7}}
	gap := OpenGaps(docWith(long))[0]
	if len([]rune(gap.Text)) > maxGapTextRunes+1 || !strings.HasSuffix(gap.Text, "…") {
		t.Fatalf("got %q", gap.Text)
	}
}

func TestFindBlockLooksInsideColumns(t *testing.T) {
	doc := docWith(columnsBlock("cols", Ratio5050, []Block{headingBlock("left", "Oi")}, nil))
	if b, ok := FindBlock(doc.Blocks, "left"); !ok || b.Heading.Text != "Oi" {
		t.Fatal("the child is found")
	}
	if _, ok := FindBlock(doc.Blocks, "ghost"); ok {
		t.Fatal("an unknown id is not found")
	}
}

func fullDocumentWithoutGaps() Document {
	return docWith(headingBlock("title", "Oi"), buttonBlock("cta", "Comprar", "https://x.com"))
}
