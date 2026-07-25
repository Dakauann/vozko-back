package customer

type CreateCustomerUseCase interface {
	CreateCustomer(userId string, name string, email string, cpf string, cnpj string) (*Customer, error)
}

type ListCustomersUseCase interface {
	ListCustomers(userId string) ([]Customer, error)
}

type GetCustomerUseCase interface {
	GetCustomer(customerID string) (*Customer, error)
}

type GetOrCreateCustomerUseCase interface {
	Execute(customer *Customer) (*Customer, bool, error)
}
