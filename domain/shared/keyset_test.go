package shared

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestAKeysetSurvivesItsOwnEncoding(t *testing.T) {
	at := time.Date(2026, 10, 8, 14, 3, 7, 123456000, time.FixedZone("BRT", -3*3600))
	for _, id := range []string{"c0a8f1e2-1111-4c1d-9d1e-000000000001", "deal:c0a8f1e2-1111-4c1d-9d1e-000000000001", "a|b"} {
		got, paged, err := ParseKeyset(Keyset{At: at, ID: id}.Encode())
		if err != nil || !paged {
			t.Fatalf("%q: paged %v err %v", id, paged, err)
		}
		if !got.At.Equal(at) || got.ID != id {
			t.Fatalf("%q came back as %+v", id, got)
		}
	}
}

func TestAnEmptyKeysetMeansTheFirstPage(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		_, paged, err := ParseKeyset(raw)
		if err != nil || paged {
			t.Fatalf("%q: paged %v err %v", raw, paged, err)
		}
	}
}

func TestAKeysetThisServerDidNotGiveIsRefused(t *testing.T) {
	encode := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	cases := map[string]string{
		"not base64":        "%%%",
		"no separator":      encode("2026-10-08T14:03:07Z"),
		"no id":             encode("2026-10-08T14:03:07Z|"),
		"blank id":          encode("2026-10-08T14:03:07Z|  "),
		"not a time":        encode("yesterday|abc"),
		"padded base64":     base64.URLEncoding.EncodeToString([]byte("2026-10-08T14:03:07Z|a")),
		"time without zone": encode("2026-10-08T14:03:07|abc"),
	}
	for name, raw := range cases {
		if _, _, err := ParseKeyset(raw); !errors.Is(err, ErrKeysetInvalid) {
			t.Errorf("%s: want ErrKeysetInvalid, got %v", name, err)
		}
	}
}
