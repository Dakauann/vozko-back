package lead

import (
	"context"
	"sort"

	"vozko/domain/shared"
)

const SharedNumberHolderLimit = 5

type NumberHolder struct {
	LeadID string
	Name   string
	Number string
}

type SharedNumber struct {
	Number  string
	Holders []NumberHolder
	More    bool
}

type NumberHolderReader interface {
	OtherHolders(ctx context.Context, workspaceID, leadID string, numbers []string) ([]*Lead, error)
}

func SharedNumbersOf(l *Lead, others []*Lead) []SharedNumber {
	numbers := []SharedNumber{}
	if l == nil {
		return numbers
	}
	for _, number := range l.Numbers() {
		holders := holdersOf(l.ID, number, others)
		if len(holders) == 0 {
			continue
		}
		entry := SharedNumber{Number: number, Holders: holders}
		if len(holders) > SharedNumberHolderLimit {
			entry.Holders, entry.More = holders[:SharedNumberHolderLimit], true
		}
		numbers = append(numbers, entry)
	}
	return numbers
}

func holdersOf(selfID, number string, others []*Lead) []NumberHolder {
	seen := map[string]bool{selfID: true}
	holders := []NumberHolder{}
	for _, other := range others {
		if other == nil || seen[other.ID] || !other.HoldsNumber(number) {
			continue
		}
		seen[other.ID] = true
		holders = append(holders, NumberHolder{LeadID: other.ID, Name: other.RealName(), Number: other.Number})
	}
	sort.SliceStable(holders, func(i, j int) bool {
		a, b := holders[i], holders[j]
		if (a.Name == "") != (b.Name == "") {
			return a.Name != ""
		}
		if an, bn := shared.FoldForMatch(a.Name), shared.FoldForMatch(b.Name); an != bn {
			return an < bn
		}
		return a.LeadID < b.LeadID
	})
	return holders
}
