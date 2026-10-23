package emailaddress

import (
	"sort"
	"strings"
)

var freeMailboxDomains = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
	"hotmail.com":    {},
	"hotmail.com.br": {},
	"outlook.com":    {},
	"outlook.com.br": {},
	"live.com":       {},
	"msn.com":        {},
	"yahoo.com":      {},
	"yahoo.com.br":   {},
	"ymail.com":      {},
	"icloud.com":     {},
	"me.com":         {},
	"mac.com":        {},
	"aol.com":        {},
	"proton.me":      {},
	"protonmail.com": {},
	"bol.com.br":     {},
	"uol.com.br":     {},
	"terra.com.br":   {},
	"ig.com.br":      {},
	"zipmail.com.br": {},
	"globo.com":      {},
	"globomail.com":  {},
}

func FreeMailboxDomains() []string {
	out := make([]string, 0, len(freeMailboxDomains))
	for domain := range freeMailboxDomains {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}

func IsFreeMailbox(domain string) bool {
	_, ok := freeMailboxDomains[strings.ToLower(strings.TrimSpace(domain))]
	return ok
}
