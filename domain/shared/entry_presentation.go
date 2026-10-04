package shared

var plainSignatureEntryTypes = map[EntryType]struct{}{
	EntryTypeInstagram: {},
	EntryTypeFacebook:  {},
	EntryTypeWebchat:   {},
}

func (e EntryType) SignsWithPlainText() bool {
	_, ok := plainSignatureEntryTypes[e]
	return ok
}

var entryTypeDisplayLabels = map[EntryType]string{
	EntryTypeInstagram: "Instagram",
	EntryTypeTelegram:  "Telegram",
	EntryTypeFacebook:  "Messenger",
	EntryTypeWebchat:   "WebChat",
}

func (e EntryType) DisplayLabel() string {
	if label, ok := entryTypeDisplayLabels[e]; ok {
		return label
	}
	return string(e)
}
