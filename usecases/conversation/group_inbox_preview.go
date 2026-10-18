package conversation_usecase

import (
	"context"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type pendingAuthor struct {
	index   int
	account string
	handle  string
}

func (s *HistoryProviderService) attributeGroupPreviews(entries []conversation.InboxEntry, rows []conversation.EntryWithLastMessage) {
	if len(entries) != len(rows) {
		return
	}

	pending := map[shared.EntryType][]pendingAuthor{}
	for i := range entries {
		if !entries[i].IsGroup {
			continue
		}
		switch rows[i].LastMessageOrigin() {
		case conversation.LastMessageByBusiness:
			entries[i].LastMessageFromUs = true
		case conversation.LastMessageByContact:
			handle := strings.TrimSpace(rows[i].LastMessageFrom)
			if handle == "" {
				continue
			}
			if strings.HasPrefix(handle, "+") {
				entries[i].LastMessageAuthor = handle
			}
			account := strings.TrimSpace(rows[i].BusinessPhoneID)
			if account == "" {
				continue
			}
			pending[rows[i].EntryType] = append(pending[rows[i].EntryType], pendingAuthor{index: i, account: account, handle: handle})
		}
	}

	for entryType, authors := range pending {
		s.nameGroupAuthors(entryType, entries, authors)
	}
}

func (s *HistoryProviderService) nameGroupAuthors(entryType shared.EntryType, entries []conversation.InboxEntry, authors []pendingAuthor) {
	lookup, ok := s.contactLookupFor(entryType)
	if !ok {
		return
	}

	handlesByAccount := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	for _, author := range authors {
		if seen[author.account] == nil {
			seen[author.account] = map[string]struct{}{}
		}
		if _, dup := seen[author.account][author.handle]; dup {
			continue
		}
		seen[author.account][author.handle] = struct{}{}
		handlesByAccount[author.account] = append(handlesByAccount[author.account], author.handle)
	}

	found, err := lookup.AuthorsByAccount(context.Background(), handlesByAccount)
	if err != nil {
		log.Printf("[HistoryProvider] %s group author lookup failed: %v", entryType, err)
		return
	}
	for _, author := range authors {
		if name, ok := readableAuthorName(found[author.account][author.handle]); ok {
			entries[author.index].LastMessageAuthor = name
		}
	}
}

func readableAuthorName(member ContactDisplay) (string, bool) {
	name := strings.TrimSpace(member.Name)
	if name == "" || name == strings.TrimSpace(member.Ref) {
		return "", false
	}
	return name, true
}
