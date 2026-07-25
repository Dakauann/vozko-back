package shared

type EntryType string

const (
	EntryTypeWhatsApp EntryType = "whatsapp"
	EntryTypeSupport  EntryType = "support"
)

func (e EntryType) Valid() bool {
	switch e {
	case EntryTypeWhatsApp, EntryTypeSupport:
		return true
	}
	return false
}

func (e EntryType) String() string {
	return string(e)
}
