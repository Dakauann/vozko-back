package customer

type CustomerRepository interface {
	CreateCustomer(customer *Customer) error
	GetCustomerByID(customerID string) (*Customer, error)
	GetCustomerByDocument(cpfCnpj string) (*Customer, error)
	GetCustomerByDocumentEmailOrPhone(cpfCnpj, email, phone string) (*Customer, error)
	GetCustomerByEmail(email string) (*Customer, error)
	GetCustomerByPhone(phone string) (*Customer, error)
	UpdateCustomer(customer *Customer) error
	ListCustomersByUser(userID string) ([]Customer, error)
}
