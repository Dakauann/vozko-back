package advertising

import (
	"errors"
	"strings"
)

var ErrAmbiguousAccount = errors.New("more than one ad account matches")

func ResolveAccount(accounts []*AdAccount, ref string) (*AdAccount, error) {
	wanted := strings.TrimSpace(ref)
	if wanted == "" {
		return nil, ErrAccountNotFound
	}
	for _, a := range accounts {
		if a.ID == wanted || NormalizeAccountID(a.MetaAccountID) == NormalizeAccountID(wanted) {
			return a, nil
		}
	}
	var named []*AdAccount
	for _, a := range accounts {
		if strings.EqualFold(strings.TrimSpace(a.Name), wanted) {
			named = append(named, a)
		}
	}
	switch len(named) {
	case 0:
		return nil, ErrAccountNotFound
	case 1:
		return named[0], nil
	}
	return nil, ErrAmbiguousAccount
}
