package emailinbox

import "testing"

func TestStripQuoted(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantBody   string
		wantQuoted string
	}{
		{
			name:     "no quote",
			in:       "Olá, quero saber do pedido.\n\nObrigado",
			wantBody: "Olá, quero saber do pedido.\n\nObrigado",
		},
		{
			name:       "gmail english",
			in:         "Thanks!\n\nOn Mon, Oct 20, 2026 at 10:00 AM Loja <loja@x.com> wrote:\n> Your order shipped",
			wantBody:   "Thanks!",
			wantQuoted: "On Mon, Oct 20, 2026 at 10:00 AM Loja <loja@x.com> wrote:\n> Your order shipped",
		},
		{
			name:       "gmail wrapped attribution",
			in:         "Valeu\n\nOn Mon, Oct 20, 2026 at 10:00 AM Loja Grande\n<loja@x.com> wrote:\n> texto",
			wantBody:   "Valeu",
			wantQuoted: "On Mon, Oct 20, 2026 at 10:00 AM Loja Grande\n<loja@x.com> wrote:\n> texto",
		},
		{
			name:       "portuguese",
			in:         "Pode enviar amanhã?\n\nEm seg., 20 de out. de 2026 às 10:00, Loja <loja@x.com> escreveu:\n> Seu pedido saiu",
			wantBody:   "Pode enviar amanhã?",
			wantQuoted: "Em seg., 20 de out. de 2026 às 10:00, Loja <loja@x.com> escreveu:\n> Seu pedido saiu",
		},
		{
			name:       "outlook original message",
			in:         "Segue.\n\n-----Original Message-----\nFrom: Loja\nSent: Monday\nSubject: Pedido",
			wantBody:   "Segue.",
			wantQuoted: "-----Original Message-----\nFrom: Loja\nSent: Monday\nSubject: Pedido",
		},
		{
			name:       "outlook portuguese header block",
			in:         "Combinado.\n\nDe: Loja <loja@x.com>\nEnviado: segunda-feira\nPara: Cliente\nAssunto: Pedido\n\ntexto antigo",
			wantBody:   "Combinado.",
			wantQuoted: "De: Loja <loja@x.com>\nEnviado: segunda-feira\nPara: Cliente\nAssunto: Pedido\n\ntexto antigo",
		},
		{
			name:     "de line without header block stays",
			in:       "De: amanhã em diante pode entregar\nobrigado",
			wantBody: "De: amanhã em diante pode entregar\nobrigado",
		},
		{
			name:       "trailing angle quote",
			in:         "Sim, confirmo.\n\n> Pode confirmar?\n> Abraço",
			wantBody:   "Sim, confirmo.",
			wantQuoted: "> Pode confirmar?\n> Abraço",
		},
		{
			name:     "interleaved reply keeps everything",
			in:       "> Qual o tamanho?\nM\n> E a cor?\nAzul",
			wantBody: "> Qual o tamanho?\nM\n> E a cor?\nAzul",
		},
		{
			name:     "only quote keeps the text as body",
			in:       "> Pode confirmar?",
			wantBody: "> Pode confirmar?",
		},
		{
			name:       "crlf normalised",
			in:         "Ok\r\n\r\nOn Mon, X wrote:\r\n> a",
			wantBody:   "Ok",
			wantQuoted: "On Mon, X wrote:\n> a",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, quoted := StripQuoted(tc.in)
			if body != tc.wantBody || quoted != tc.wantQuoted {
				t.Errorf("StripQuoted() = (%q, %q), want (%q, %q)", body, quoted, tc.wantBody, tc.wantQuoted)
			}
		})
	}
}
