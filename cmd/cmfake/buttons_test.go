package main

import (
	"reflect"
	"testing"
)

func TestButtonsOf(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []button
	}{
		{"plain text", `{"richContent":{"conversation":[{"text":"oi"}]}}`, nil},
		{"sms", `{"body":{"content":"oi"}}`, nil},
		{"chips", `{"richContent":{"conversation":[{"text":"oi"}],"suggestions":[
			{"action":"Reply","label":"Quero","postbackdata":"vz:m0"},
			{"action":"OpenUrl","label":"Site","url":"https://example.com","postbackdata":"vz:m1"}]}}`,
			[]button{{Label: "Quero", Postback: "vz:m0", Action: "Reply"}, {Label: "Site", Postback: "vz:m1", Action: "OpenUrl"}}},
		{"rich card", `{"richContent":{"conversation":[{"header":"A","suggestions":[{"action":"Dial","label":"Ligar","postbackdata":"vz:c0b0"}]}]}}`,
			[]button{{Label: "Ligar", Postback: "vz:c0b0", Action: "Dial"}}},
		{"carousel and chips", `{"richContent":{"conversation":[{"carousel":{"cardWidth":"Medium","cards":[
			{"header":"A","suggestions":[{"action":"Reply","label":"A","postbackdata":"vz:c0b0"}]},
			{"header":"B","suggestions":[{"action":"Reply","label":"B","postbackdata":"vz:c1b0"}]}]}}],
			"suggestions":[{"action":"Reply","label":"Nenhum","postbackdata":"vz:m0"}]}}`,
			[]button{{Label: "Nenhum", Postback: "vz:m0", Action: "Reply"}, {Label: "A", Postback: "vz:c0b0", Action: "Reply"}, {Label: "B", Postback: "vz:c1b0", Action: "Reply"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buttonsOf([]byte(tc.raw))
			if err != nil {
				t.Fatalf("buttonsOf: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
	if _, err := buttonsOf([]byte(`{"richContent":`)); err == nil {
		t.Fatalf("broken json must fail")
	}
}
