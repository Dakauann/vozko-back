package mercadolivre

const (
	MaxBlacklistLimit = 50
	MaxBlacklistPages = 40
)

type BlacklistPage struct {
	UserIDs []string
	Total   int
	Offset  int
	Limit   int
}

type BlacklistScan struct {
	seen       map[string]struct{}
	ids        []string
	total      int
	read       int
	pages      int
	consistent bool
	exhausted  bool
}

func NewBlacklistScan() *BlacklistScan {
	return &BlacklistScan{seen: map[string]struct{}{}, consistent: true}
}

func (s *BlacklistScan) Add(page BlacklistPage) (next int, more bool) {
	s.pages++
	if s.pages == 1 {
		s.total = page.Total
	} else if page.Total != s.total {
		s.consistent = false
	}
	for _, id := range page.UserIDs {
		if !ValidNumericID(id) {
			s.consistent = false
			continue
		}
		if _, dup := s.seen[id]; dup {
			s.consistent = false
			continue
		}
		s.seen[id] = struct{}{}
		s.ids = append(s.ids, id)
	}
	s.read += len(page.UserIDs)
	if s.read >= s.total {
		s.exhausted = true
		return s.read, false
	}
	if len(page.UserIDs) == 0 || s.pages >= MaxBlacklistPages {
		return s.read, false
	}
	return s.read, true
}

func (s *BlacklistScan) UserIDs() []string { return s.ids }

func (s *BlacklistScan) Complete() bool {
	return s.consistent && s.exhausted && len(s.ids) == s.total
}
