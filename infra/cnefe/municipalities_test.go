package cnefe

import (
	"strings"
	"testing"
)

func TestReadMunicipalities(t *testing.T) {
	body := `[{"municipio-id":1400100,"municipio-nome":"Boa Vista","UF-id":14,"UF-sigla":"RR"},` +
		`{"municipio-id":1100015,"municipio-nome":"Alta Floresta D'Oeste","UF-sigla":"RO"}]`
	got, err := ReadMunicipalities(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ReadMunicipalities() err = %v", err)
	}
	if len(got) != 2 || got["1400100"].Name != "Boa Vista" || got["1400100"].State != "RR" || got["1100015"].Name != "Alta Floresta D'Oeste" {
		t.Fatalf("ReadMunicipalities() = %+v", got)
	}
}

func TestReadMunicipalitiesRefusesAnEmptyOrBrokenDirectory(t *testing.T) {
	for _, body := range []string{"[]", "{", `[{"municipio-id":0,"municipio-nome":"X","UF-sigla":"RR"}]`} {
		if _, err := ReadMunicipalities(strings.NewReader(body)); err == nil {
			t.Fatalf("ReadMunicipalities(%q) must refuse", body)
		}
	}
}
