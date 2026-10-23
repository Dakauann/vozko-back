package emailinbox

import "testing"

func TestNormalizeSubject(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "Pedido 123", "Pedido 123"},
		{"re", "Re: Pedido 123", "Pedido 123"},
		{"res portuguese", "RES: Pedido 123", "Pedido 123"},
		{"stacked", "Re: RES: Fwd: ENC: RV: Pedido", "Pedido"},
		{"case and spacing", "  re :  fw:Pedido   123  ", "Pedido 123"},
		{"counted", "Re[2]: Pedido", "Pedido"},
		{"tr and aw", "TR: AW: Pedido", "Pedido"},
		{"only prefixes", "Re: Fwd:", ""},
		{"empty", "", ""},
		{"word starting like prefix", "Resumo do pedido", "Resumo do pedido"},
		{"newlines folded", "Re: Pedido\r\n 123", "Pedido 123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeSubject(tc.in); got != tc.want {
				t.Errorf("NormalizeSubject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReplySubject(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Pedido 123", "Re: Pedido 123"},
		{"RES: Re: Pedido 123", "Re: Pedido 123"},
		{"", "Re:"},
	}
	for _, tc := range cases {
		if got := ReplySubject(tc.in); got != tc.want {
			t.Errorf("ReplySubject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
