package media_test

import (
	"testing"

	"vozko/domain/media"
)

func TestDisplayNamePrefersTheUploadedName(t *testing.T) {
	cases := map[string]media.Media{
		"lista.csv":         {Description: " lista.csv ", URL: "https://files.test/ws/1f2e.csv"},
		"Banner.jpg":        {Description: "Banner", URL: "https://files.test/ws/9a8b.jpg"},
		"1f2e.pdf":          {URL: "https://files.test/ws/1f2e.pdf"},
		"Tabela final.xlsx": {Description: "Tabela final.xlsx", URL: "https://files.test/ws/3c4d.xlsx"},
	}
	for want, m := range cases {
		if got := m.DisplayName(); got != want {
			t.Fatalf("%+v = %q, want %q", m, got, want)
		}
	}
}
