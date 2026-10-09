package lead

import "errors"

var ErrAggregateNotLoaded = errors.New("lead: the lead was read without its phones and addresses, so it cannot be saved")

func (l *Lead) IsAggregate() bool {
	return l.Phones != nil && l.Addresses != nil
}

func (l *Lead) EnsureCollections() {
	if l.Phones == nil {
		l.Phones = []ContactPhone{}
	}
	if l.Addresses == nil {
		l.Addresses = []Address{}
	}
	if l.Relations == nil {
		l.Relations = []Relation{}
	}
}
