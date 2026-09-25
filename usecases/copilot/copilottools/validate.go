package copilottools

import (
	"fmt"
	"reflect"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
)

func validateArgs[T any](entries conversation.EntryLookup, cc copilot.Context, args map[string]interface{}) (T, error) {
	var a T
	if err := decodeArgs(args, &a); err != nil {
		return a, err
	}
	entryID, entryType, ok := entryRef(&a)
	if !ok || entryID == "" {
		return a, nil
	}
	return a, requireConversation(entries, cc, entryID, entryType)
}

func entryRef(dst any) (string, string, bool) {
	value := reflect.ValueOf(dst).Elem()
	var id, kind string
	found := false
	for _, f := range reflect.VisibleFields(value.Type()) {
		switch jsonName(f) {
		case "entry_id":
			id, found = value.FieldByIndex(f.Index).String(), true
		case "entry_type":
			kind = value.FieldByIndex(f.Index).String()
		}
	}
	return id, kind, found
}

func requireConversation(entries conversation.EntryLookup, cc copilot.Context, entryID, entryType string) error {
	target, err := targetOf(entryID, entryType)
	if err != nil {
		return err
	}
	if entries != nil {
		if entry, err := entries.LookupEntry(viewerOf(cc), target.EntryID, target.EntryType); err == nil && entry != nil {
			return nil
		}
	}
	return fmt.Errorf("%w: conversa não encontrada ou sem acesso; use o entry_id e o entry_type exatos de search_conversations", errInvalidArgs)
}
