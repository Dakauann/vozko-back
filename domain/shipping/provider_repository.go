package shipping

type ProviderAccountRepository interface {
	Create(account *ProviderAccount) error
	Update(account *ProviderAccount) error
	FindByID(id string) (*ProviderAccount, error)
	ListByWorkspace(userID string, provider Provider) ([]*ProviderAccount, error)
}
