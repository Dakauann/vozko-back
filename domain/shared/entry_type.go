package shared

type EntryType string

const (
	EntryTypeWhatsApp  EntryType = "whatsapp"
	EntryTypeSupport   EntryType = "support"
	EntryTypeInstagram EntryType = "instagram"
)

func (e EntryType) Valid() bool {
	switch e {
	case EntryTypeWhatsApp, EntryTypeSupport, EntryTypeInstagram:
		return true
	}
	return false
}

func (e EntryType) String() string {
	return string(e)
}
