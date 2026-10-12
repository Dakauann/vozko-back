package studio

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

type generatedLayerCase struct {
	Name  string          `json:"name"`
	Layer json.RawMessage `json:"layer"`
	Code  *string         `json:"code"`
}

func TestTheEditorTableAndQRDocumentsPassTheContract(t *testing.T) {
	for kind, name := range map[Kind]string{KindImage: "testdata/editor_table_qr.json", KindVideo: "testdata/editor_table_qr_video.json"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDocument(kind, raw); err != nil {
			t.Fatalf("the editor saved a table and qr document the backend refuses (%s): %v", name, err)
		}
	}
}

func layersIn(t *testing.T, name string) []Layer {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Layers    []Layer `json:"layers"`
		Artboards []struct {
			Layers []Layer `json:"layers"`
		} `json:"artboards"`
		Tracks []struct {
			Clips []struct {
				Layer *Layer `json:"layer"`
			} `json:"clips"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	layers := append([]Layer{}, doc.Layers...)
	for _, board := range doc.Artboards {
		layers = append(layers, board.Layers...)
	}
	for _, track := range doc.Tracks {
		for _, clip := range track.Clips {
			if clip.Layer != nil {
				layers = append(layers, *clip.Layer)
			}
		}
	}
	return layers
}

func TestTheTableAndQRSpecsKeepTheirData(t *testing.T) {
	for _, name := range []string{"testdata/editor_table_qr.json", "testdata/editor_table_qr_video.json"} {
		var table *TableSpec
		var logo *QRLogo
		message := ""
		for _, layer := range layersIn(t, name) {
			if layer.Table != nil && table == nil {
				table = layer.Table
			}
			if layer.QR != nil && layer.QR.Logo != nil {
				logo = layer.QR.Logo
			}
			if layer.QR != nil && layer.QR.Link != nil && layer.QR.Link.Kind == QRLinkWhatsapp {
				message = layer.QR.Link.Message
			}
		}
		if table == nil || len(table.Rows) == 0 || len(table.Columns) == 0 {
			t.Fatalf("%s: the table was lost: %+v", name, table)
		}
		run := false
		for _, row := range table.Rows {
			for _, cell := range row {
				if len(cell.Runs) > 0 && cell.Runs[0].End > cell.Runs[0].Start && cell.Runs[0].styled() {
					run = true
				}
			}
		}
		if !run || table.Style.FontID == "" || table.Style.Borders == "" {
			t.Fatalf("%s: the table cells or style were lost: %+v", name, table)
		}
		if name == "testdata/editor_table_qr.json" && (logo == nil || logo.Scale <= 0 || logo.AssetID != "media-logo") {
			t.Fatalf("%s: the qr logo was lost: %+v", name, logo)
		}
		if name == "testdata/editor_table_qr.json" && message != "Olá! Vi o anúncio\nquero saber mais" {
			t.Fatalf("%s: the whatsapp message was lost: %q", name, message)
		}
		if name == "testdata/editor_table_qr_video.json" && !hasShortLink(layersIn(t, name)) {
			t.Fatalf("%s: the qr short link was lost", name)
		}
	}
}

func hasShortLink(layers []Layer) bool {
	for _, layer := range layers {
		if layer.QR != nil && layer.QR.Link != nil && layer.QR.Link.Kind == QRLinkShort && layer.QR.Link.ShortLinkID != "" && layer.QR.Link.URL != "" {
			return true
		}
	}
	return false
}

func TestGeneratedLayerCasesMatchTheEditor(t *testing.T) {
	raw, err := os.ReadFile("testdata/generated_layer_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []generatedLayerCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no generated cases")
	}
	for _, c := range cases {
		var layer Layer
		dec := json.NewDecoder(bytes.NewReader(c.Layer))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&layer); err != nil {
			t.Errorf("%s: the layer does not decode: %v", c.Name, err)
			continue
		}
		want := ""
		if c.Code != nil {
			want = *c.Code
		}
		doc := imageDoc()
		doc.Layers = append(doc.Layers, layer)
		got := ""
		if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
			got = codeOf(t, err)[FieldLayers]
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", c.Name, got, want)
		}
	}
}

func TestWhatsappContentEscapesLikeTheBrowser(t *testing.T) {
	link := QRLink{Kind: QRLinkWhatsapp, Phone: "5511999998888", Message: "Olá, quero o plano Pro! (50% off) ~*'"}
	want := "https://wa.me/5511999998888?text=Ol%C3%A1%2C%20quero%20o%20plano%20Pro!%20(50%25%20off)%20~*'"
	if got := link.content(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	bare := QRLink{Kind: QRLinkWhatsapp, Phone: "5511999998888"}
	if got := bare.content(); got != "https://wa.me/5511999998888" {
		t.Fatalf("got %q", got)
	}
}
