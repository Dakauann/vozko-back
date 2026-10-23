package scriptvm

import (
	"testing"

	"github.com/dop251/goja"
)

func bodyOf(t *testing.T, options string) string {
	t.Helper()
	vm := goja.New()
	arg, err := vm.RunString("(" + options + ")")
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := arg.Export().(map[string]interface{})
	return requestBody(arg, decoded["body"])
}

func TestABinaryBodyIsSentAsItsBytes(t *testing.T) {
	want := "%PDF\xff\x00\x80"
	for name, options := range map[string]string{
		"Uint8Array":  `{body: new Uint8Array([37, 80, 68, 70, 255, 0, 128])}`,
		"ArrayBuffer": `{body: new Uint8Array([37, 80, 68, 70, 255, 0, 128]).buffer}`,
	} {
		if got := bodyOf(t, options); got != want {
			t.Fatalf("%s: body = %q, want %q", name, got, want)
		}
	}
}

func TestTextAndObjectBodiesKeepTheirEncoding(t *testing.T) {
	if got := bodyOf(t, `{body: "a=1"}`); got != "a=1" {
		t.Fatalf("string body = %q", got)
	}
	if got := bodyOf(t, `{body: {ola: "mundo"}}`); got != `{"ola":"mundo"}` {
		t.Fatalf("object body = %q", got)
	}
	if got := bodyOf(t, `{url: "https://x"}`); got != "" {
		t.Fatalf("no body = %q", got)
	}
}
