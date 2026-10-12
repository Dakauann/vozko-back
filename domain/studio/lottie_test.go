package studio

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLottieFixturesAreReadable(t *testing.T) {
	files, err := filepath.Glob("testdata/lottie/*")
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, file := range files {
		if strings.HasSuffix(file, ".clips.json") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := ParseLottie(filepath.Base(file), raw); err != nil {
			t.Errorf("%s: %v", file, err)
		}
		read++
	}
	if read < 9 {
		t.Fatalf("only %d Lottie fixtures were read", read)
	}
}

func TestConvertedLottieFixturesAreValidDocuments(t *testing.T) {
	files, err := filepath.Glob("testdata/lottie/*.clips.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no converted Lottie fixture; run bun run studio:library")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDocument(KindVideo, raw); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
}

func dotLottie(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseLottieRefusesWhatIsNotAnAnimation(t *testing.T) {
	valid := `{"v":"5.7.4","fr":30,"ip":0,"op":30,"w":100,"h":100,"layers":[]}`
	cases := []struct {
		name string
		file string
		data []byte
		ok   bool
	}{
		{"plain json", "a.json", []byte(valid), true},
		{"upper case extension", "a.JSON", []byte(valid), true},
		{"wrong extension", "a.txt", []byte(valid), false},
		{"broken json", "a.json", []byte("{"), false},
		{"no frame rate", "a.json", []byte(`{"ip":0,"op":30,"w":100,"h":100,"layers":[]}`), false},
		{"out before in", "a.json", []byte(`{"fr":30,"ip":10,"op":10,"w":100,"h":100,"layers":[]}`), false},
		{"no layers", "a.json", []byte(`{"fr":30,"ip":0,"op":10,"w":100,"h":100}`), false},
		{"dotlottie", "a.lottie", dotLottie(t, map[string]string{"manifest.json": `{"animations":[{"id":"x"}]}`, "animations/x.json": valid}), true},
		{"dotlottie v2 folder", "a.lottie", dotLottie(t, map[string]string{"manifest.json": `{"animations":[{"id":"x"}]}`, "a/x.json": valid}), true},
		{"dotlottie without manifest", "a.lottie", dotLottie(t, map[string]string{"animations/x.json": valid}), false},
		{"dotlottie with a broken animation", "a.lottie", dotLottie(t, map[string]string{"manifest.json": `{"animations":[{"id":"x"}]}`, "animations/x.json": "{}"}), false},
		{"not a zip", "a.lottie", []byte(valid), false},
	}
	for _, tc := range cases {
		err := ParseLottie(tc.file, tc.data)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}
