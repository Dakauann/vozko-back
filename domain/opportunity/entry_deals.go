package opportunity

import "errors"

var (
	ErrAmbiguousDeal = errors.New("opportunity: the conversation has more than one open deal in this funnel")
	ErrDealNotLinked = errors.New("opportunity: the deal is not linked to this conversation in this funnel")
	ErrDealClosed    = errors.New("opportunity: the deal is already closed")
)

type EntryDeals []*Opportunity

func (d EntryDeals) Open() EntryDeals {
	open := make(EntryDeals, 0, len(d))
	for _, o := range d {
		if o.Status == StatusOpen {
			open = append(open, o)
		}
	}
	return open
}

func (d EntryDeals) Editable(id string) (*Opportunity, error) {
	if id != "" {
		o, err := d.linked(id)
		if err != nil {
			return nil, err
		}
		if o.Status != StatusOpen {
			return nil, ErrDealClosed
		}
		return o, nil
	}
	return d.Open().only()
}

func (d EntryDeals) Current(id string) (*Opportunity, error) {
	if id != "" {
		return d.linked(id)
	}
	if open := d.Open(); len(open) > 0 {
		return open.only()
	}
	return d.newest()
}

func (d EntryDeals) linked(id string) (*Opportunity, error) {
	for _, o := range d {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, ErrDealNotLinked
}

func (d EntryDeals) only() (*Opportunity, error) {
	switch len(d) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return d[0], nil
	}
	return nil, ErrAmbiguousDeal
}

func (d EntryDeals) newest() (*Opportunity, error) {
	var newest *Opportunity
	for _, o := range d {
		if newest == nil || o.CreatedAt.After(newest.CreatedAt) {
			newest = o
		}
	}
	if newest == nil {
		return nil, ErrNotFound
	}
	return newest, nil
}
