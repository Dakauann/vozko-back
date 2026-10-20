package redact

import (
	"reflect"
	"strings"
	"testing"
)

func TestSensitiveKeysAreMatchedCaseInsensitivelyIgnoringSeparators(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"Senha", true},
		{"unifecaf_secret", true},
		{"_node_n55_accessToken", true},
		{"access_token", true},
		{"refresh_token", true},
		{"Authorization", true},
		{"Set-Cookie", true},
		{"api_key", true},
		{"apikey", true},
		{"X-Api-Key", true},
		{"smtp_credential", true},
		{"status_code", false},
		{"nome", false},
		{"_engine_output_node_id", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			if got := IsSensitiveKey(c.key); got != c.want {
				t.Fatalf("IsSensitiveKey(%q) = %v, want %v", c.key, got, c.want)
			}
		})
	}
}

func TestTokenCountersStayReadableBecauseAIUsageIsDebuggingData(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"prompt_tokens", false},
		{"completion_tokens", false},
		{"max_tokens", false},
		{"totalTokens", false},
		{"tokens_used", false},
		{"accessToken", true},
		{"tokens_secret", true},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			if got := IsSensitiveKey(c.key); got != c.want {
				t.Fatalf("IsSensitiveKey(%q) = %v, want %v", c.key, got, c.want)
			}
		})
	}
}

func TestMapMasksSensitiveKeysAtAnyDepthAndKeepsTheShape(t *testing.T) {
	in := map[string]interface{}{
		"status_code": 200,
		"accessToken": "eyJhbGciOi",
		"empty_token": "",
		"user": map[string]interface{}{
			"name":  "Ana",
			"senha": 1234,
			"items": []interface{}{
				map[string]interface{}{"api_key": "k1", "id": "a"},
				"plain",
			},
		},
		"headers": map[string]string{"Authorization": "Bearer abc", "Accept": "json"},
	}
	got := New().Map(in)
	want := map[string]interface{}{
		"status_code": 200,
		"accessToken": Mask,
		"empty_token": "",
		"user": map[string]interface{}{
			"name":  "Ana",
			"senha": Mask,
			"items": []interface{}{
				map[string]interface{}{"api_key": Mask, "id": "a"},
				"plain",
			},
		},
		"headers": map[string]string{"Authorization": Mask, "Accept": "json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestStringListsKeepNilAndMaskEachEntry(t *testing.T) {
	if New().Strings(nil) != nil {
		t.Fatal("a missing list must stay missing so it is never stored as an empty one")
	}
	got := SecretsOnly("tok-1").Strings([]string{"login failed with tok-1", "ok"})
	if len(got) != 2 || got[0] != "login failed with "+Mask || got[1] != "ok" {
		t.Fatalf("got = %v", got)
	}
	if v, ok := New().Value([]string(nil)).([]string); !ok || v != nil {
		t.Fatalf("Value of a nil list = %#v", v)
	}
}

func TestRedactionNeverMutatesItsInput(t *testing.T) {
	nested := map[string]interface{}{"token": "t1"}
	list := []interface{}{"123.456.789-09"}
	in := map[string]interface{}{"nested": nested, "list": list, "password": "p"}
	_ = New("t1").Map(in)
	if nested["token"] != "t1" || list[0] != "123.456.789-09" || in["password"] != "p" {
		t.Fatalf("input changed: %#v", in)
	}
}

func TestSecretValuesAreMaskedAsWholeStringsAndSubstrings(t *testing.T) {
	r := New("s3cr3t-value", "p@ss word")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"whole string", "s3cr3t-value", Mask},
		{"substring", "login with s3cr3t-value now", "login with " + Mask + " now"},
		{"query escaped form", "https://x.test/a?k=p%40ss+word", "https://x.test/a?k=" + Mask},
		{"path escaped form", "https://x.test/p@ss%20word/x", "https://x.test/" + Mask + "/x"},
		{"untouched text", "nothing here", "nothing here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.String(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestEmptySecretValuesAreIgnored(t *testing.T) {
	if got := New("", "").String("abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestCredentialsEmbeddedInStringsAreMasked(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"json body", `{"accessToken":"eyJ.x.y","nome":"Ana"}`, `{"accessToken":"` + Mask + `","nome":"Ana"}`},
		{"json with spaces and numbers", `{"senha" : 1234, "id": 7}`, `{"senha" : "` + Mask + `", "id": 7}`},
		{"error text with a body", `HTTP 401: {"token": "abc"}`, `HTTP 401: {"token": "` + Mask + `"}`},
		{"query string", "https://x.test/a?api_key=abc&page=2", "https://x.test/a?api_key=" + Mask + "&page=2"},
		{"form body", "username=ana&password=xyz", "username=ana&password=" + Mask},
		{"bearer header text", "Authorization: Bearer abc.def-ghi", "Authorization: Bearer " + Mask},
		{"plain prose", "the token is valid", "the token is valid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New().String(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestDocumentsKeepOnlyTheLastTwoDigits(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"formatted cpf", "cpf 123.456.789-09 ok", "cpf •••.•••.•••-09 ok"},
		{"digits only cpf with valid check digits", "cpf=52998224725", "cpf=•••••••••25"},
		{"formatted cnpj", "11.222.333/0001-81", "••.•••.•••/••••-81"},
		{"digits only cnpj with valid check digits", "cnpj 11222333000181.", "cnpj ••••••••••••81."},
		{"formatted cpf is masked even with wrong check digits", "111.222.333-44", "•••.•••.•••-44"},
		{"eleven digits that are not a cpf stay", "11987654321", "11987654321"},
		{"phone with country code stays", "5511987654321", "5511987654321"},
		{"date stays", "2026-10-19", "2026-10-19"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New().String(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestSecretsOnlyMasksSecretValuesButKeepsOtherCredentialsForTheRun(t *testing.T) {
	in := map[string]interface{}{
		"accessToken": "eyJ.real",
		"body":        `{"password":"s3cr3t"}`,
		"cpf":         "123.456.789-09",
		"list":        []interface{}{"x s3cr3t y"},
	}
	got := SecretsOnly("s3cr3t").Map(in)
	want := map[string]interface{}{
		"accessToken": "eyJ.real",
		"body":        `{"password":"` + Mask + `"}`,
		"cpf":         "123.456.789-09",
		"list":        []interface{}{"x " + Mask + " y"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestValueHandlesEveryShapeAndPassesOthersThrough(t *testing.T) {
	r := New("zz")
	if got := r.Value("a zz"); got != "a "+Mask {
		t.Fatalf("string: %v", got)
	}
	if got := r.Value([]string{"zz", "ok"}); !reflect.DeepEqual(got, []string{Mask, "ok"}) {
		t.Fatalf("[]string: %v", got)
	}
	if got := r.Value(42); got != 42 {
		t.Fatalf("int: %v", got)
	}
	if got := r.Value(nil); got != nil {
		t.Fatalf("nil: %v", got)
	}
	if got := r.Map(nil); got != nil {
		t.Fatalf("nil map: %v", got)
	}
}

func TestRedactionIsLinearOnLargePayloads(t *testing.T) {
	big := strings.Repeat(`{"nome":"Ana","token":"abc"},`, 20000)
	out := New("abc").String(big)
	if strings.Contains(out, "abc") {
		t.Fatal("secret survived")
	}
}
