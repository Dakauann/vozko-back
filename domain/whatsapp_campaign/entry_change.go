package whatsapp_campaign

func (in UpdateEntryInput) ChangesTheRecipient() bool {
	return in.Number != nil || in.Name != nil || in.Variables != nil || in.Metadata != nil
}
