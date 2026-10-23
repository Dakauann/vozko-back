package emaildesign

import (
	"strings"
	"testing"
)

func TestAnInertUnsubscribeKeepsTheLabelButLinksNowhere(t *testing.T) {
	in := fullInput()
	in.UnsubscribeURL = ""
	in.InertUnsubscribe = true
	out, err := Render(fullDocument(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.HTML, `<a href="#"`) || !strings.Contains(out.HTML, ">Descadastrar</a>") {
		t.Fatalf("the footer lost its inert unsubscribe link: %s", out.HTML)
	}
	if strings.Contains(out.HTML, "mail.example.com/u") || strings.Contains(out.Text, "mail.example.com/u") || !strings.Contains(out.Text, "Descadastrar") {
		t.Fatalf("an inert render still carries a working unsubscribe url: %s", out.Text)
	}
}

func TestAnInertUnsubscribeNeverPrintsTheRealURLEvenWhenGiven(t *testing.T) {
	in := fullInput()
	in.InertUnsubscribe = true
	out, err := Render(fullDocument(), in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.HTML, in.UnsubscribeURL) || strings.Contains(out.Text, in.UnsubscribeURL) {
		t.Fatal("an inert render printed the live unsubscribe url")
	}
}
