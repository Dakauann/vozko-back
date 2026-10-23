package emailhygiene

import (
	"strings"

	"vozko/domain/emailaddress"
)

var roleLocalParts = map[string]struct{}{
	"contato": {}, "contatos": {}, "vendas": {}, "venda": {}, "financeiro": {}, "comercial": {}, "atendimento": {},
	"suporte": {}, "sac": {}, "rh": {}, "marketing": {}, "mkt": {}, "adm": {}, "admin": {}, "administrativo": {},
	"administracao": {}, "compras": {}, "faturamento": {}, "cobranca": {}, "diretoria": {}, "secretaria": {},
	"recepcao": {}, "ouvidoria": {}, "juridico": {}, "fiscal": {}, "contabilidade": {}, "orcamento": {},
	"orcamentos": {}, "pedidos": {}, "loja": {}, "imprensa": {}, "comunicacao": {}, "vagas": {}, "curriculos": {},
	"info": {}, "informacoes": {}, "sales": {}, "support": {}, "contact": {}, "billing": {}, "help": {},
	"office": {}, "hello": {}, "team": {}, "equipe": {}, "noreply": {}, "no-reply": {}, "naoresponda": {},
	"nao-responda": {}, "donotreply": {}, "do-not-reply": {}, "postmaster": {}, "abuse": {}, "webmaster": {},
	"hostmaster": {}, "root": {}, "privacy": {}, "security": {}, "newsletter": {}, "careers": {}, "jobs": {},
}

func IsRole(a emailaddress.Address) bool {
	local := a.LocalPart()
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	_, ok := roleLocalParts[strings.ToLower(local)]
	return ok
}
