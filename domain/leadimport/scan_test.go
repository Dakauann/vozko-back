package leadimport

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestScanReadsTheHeaderASampleAndTheRowCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBFtelefone;nome\n\n")
	for i := 0; i < 8; i++ {
		b.WriteString("1198765432")
		b.WriteByte(byte('0' + i))
		b.WriteString(";Pessoa\n")
	}
	got, err := ScanFile([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Headers, []string{"telefone", "nome"}) || got.Rows != 8 || len(got.Sample) != SampleRows || got.Sample[0][0] != "11987654320" {
		t.Fatalf("scan = %+v", got)
	}
}

func TestScanRefusesWhatIsNotATextSheet(t *testing.T) {
	cases := []struct {
		name string
		data string
		want error
	}{
		{"an xlsx file", "PK\x03\x04\x14\x00rest", ErrUnsupportedFile},
		{"binary bytes", "nome\x00numero\n1\x002\n", ErrUnsupportedFile},
		{"nothing", "\n \n", ErrFileEmpty},
		{"only a header", "telefone;nome\n", ErrFileEmpty},
		{"too many columns", strings.Repeat("c;", MaxHeaderCells) + "c\n1\n", ErrUnsupportedFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ScanFile([]byte(tc.data)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestScanStopsPastTheRowCap(t *testing.T) {
	data := "telefone\n" + strings.Repeat("11987654321\n", MaxRows+1)
	if _, err := ScanFile([]byte(data)); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("err = %v, want ErrTooManyRows", err)
	}
	if _, err := ScanFile(make([]byte, MaxFileBytes+1)); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err = %v, want ErrFileTooLarge", err)
	}
}
