package emailhygiene

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/emailaddress"
)

const MinDisposableDomains = 5000

var ErrDisposableListInvalid = errors.New("emailhygiene: the disposable domain list is not usable")

type DisposableSet struct {
	domains map[string]struct{}
}

func ParseDisposableList(content string) (*DisposableSet, error) {
	set := &DisposableSet{domains: map[string]struct{}{}}
	scanner := bufio.NewScanner(strings.NewReader(content))
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		domain, err := emailaddress.NormalizeDomain(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: line %d is not a domain", ErrDisposableListInvalid, line)
		}
		set.domains[domain] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDisposableListInvalid, err)
	}
	if len(set.domains) < MinDisposableDomains {
		return nil, fmt.Errorf("%w: %d domains, at least %d expected", ErrDisposableListInvalid, len(set.domains), MinDisposableDomains)
	}
	return set, nil
}

func (s *DisposableSet) Contains(domain string) bool {
	if s == nil {
		return false
	}
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	for strings.Contains(d, ".") {
		if _, ok := s.domains[d]; ok {
			return true
		}
		d = d[strings.IndexByte(d, '.')+1:]
	}
	return false
}

func (s *DisposableSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.domains)
}
