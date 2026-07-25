package customer_usecase

import (
	"errors"
	"testing"

	"vozko/domain/customer"
)

type mockCustomerRepository struct {
	customers    map[string]*customer.Customer
	createErr    error
	updateErr    error
	findByDocErr error
}

func newMockRepo() *mockCustomerRepository {
	return &mockCustomerRepository{
		customers: make(map[string]*customer.Customer),
	}
}

func (m *mockCustomerRepository) CreateCustomer(c *customer.Customer) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.customers[c.ID] = c
	return nil
}

func (m *mockCustomerRepository) GetCustomerByID(customerID string) (*customer.Customer, error) {
	c, ok := m.customers[customerID]
	if !ok {
		return nil, nil
	}
	return c, nil
}

func (m *mockCustomerRepository) GetCustomerByDocument(cpfCnpj string) (*customer.Customer, error) {
	if m.findByDocErr != nil {
		return nil, m.findByDocErr
	}
	for _, c := range m.customers {
		if c.CPF == cpfCnpj || c.CNPJ == cpfCnpj {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockCustomerRepository) GetCustomerByDocumentEmailOrPhone(cpfCnpj, email, phone string) (*customer.Customer, error) {
	if m.findByDocErr != nil {
		return nil, m.findByDocErr
	}
	for _, c := range m.customers {
		if c.CPF == cpfCnpj || c.CNPJ == cpfCnpj || c.Email == email || c.MobileNumber == phone {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockCustomerRepository) GetCustomerByEmail(email string) (*customer.Customer, error) {
	for _, c := range m.customers {
		if c.Email == email {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockCustomerRepository) GetCustomerByPhone(phone string) (*customer.Customer, error) {
	for _, c := range m.customers {
		if c.MobileNumber == phone {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockCustomerRepository) UpdateCustomer(c *customer.Customer) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.customers[c.ID] = c
	return nil
}

func (m *mockCustomerRepository) ListCustomersByUser(userID string) ([]customer.Customer, error) {
	var result []customer.Customer
	for _, c := range m.customers {
		if c.UserID == userID {
			result = append(result, *c)
		}
	}
	return result, nil
}

func TestGetOrCreateCustomerUseCase_CreateNew(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	newCustomer := &customer.Customer{
		Name:         "João Silva",
		Email:        "joao@example.com",
		MobileNumber: "11999998888",
		CPF:          "70658062433",
	}

	result, isNew, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !isNew {
		t.Error("Expected isNew to be true for new customer")
	}

	if result.ID == "" {
		t.Error("Expected customer ID to be set")
	}

	if result.Name != "João Silva" {
		t.Errorf("Expected name 'João Silva', got '%s'", result.Name)
	}

	if len(repo.customers) != 1 {
		t.Errorf("Expected 1 customer in repo, got %d", len(repo.customers))
	}
}

func TestGetOrCreateCustomerUseCase_FindByCPF(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	existingCustomer := &customer.Customer{
		ID:           "existing-id",
		Name:         "João Existente",
		Email:        "joao.old@example.com",
		MobileNumber: "11888887777",
		CPF:          "70658062433",
	}
	repo.customers["existing-id"] = existingCustomer

	newCustomer := &customer.Customer{
		Name:         "João Novo",
		Email:        "joao.new@example.com",
		MobileNumber: "11999998888",
		CPF:          "70658062433",
	}

	result, isNew, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if isNew {
		t.Error("Expected isNew to be false when finding existing customer")
	}

	if result.ID != "existing-id" {
		t.Errorf("Expected existing customer ID 'existing-id', got '%s'", result.ID)
	}

	if result.Name != "João Novo" {
		t.Errorf("Expected name to be updated to 'João Novo', got '%s'", result.Name)
	}

	if result.Email != "joao.new@example.com" {
		t.Errorf("Expected email to be updated, got '%s'", result.Email)
	}
}

func TestGetOrCreateCustomerUseCase_FindByEmail(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	existingCustomer := &customer.Customer{
		ID:           "email-id",
		Name:         "Maria Email",
		Email:        "maria@example.com",
		MobileNumber: "11777776666",
	}
	repo.customers["email-id"] = existingCustomer

	newCustomer := &customer.Customer{
		Name:  "Maria Nova",
		Email: "maria@example.com",
		CPF:   "12345678900",
	}

	result, isNew, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if isNew {
		t.Error("Expected isNew to be false when finding by email")
	}

	if result.ID != "email-id" {
		t.Errorf("Expected existing customer ID 'email-id', got '%s'", result.ID)
	}

	if result.CPF != "12345678900" {
		t.Errorf("Expected CPF to be updated, got '%s'", result.CPF)
	}
}

func TestGetOrCreateCustomerUseCase_FindByPhone(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	existingCustomer := &customer.Customer{
		ID:           "phone-id",
		Name:         "Pedro Phone",
		MobileNumber: "11555554444",
	}
	repo.customers["phone-id"] = existingCustomer

	newCustomer := &customer.Customer{
		Name:         "Pedro Novo",
		MobileNumber: "11555554444",
		CPF:          "98765432100",
		Email:        "pedro@example.com",
	}

	result, isNew, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if isNew {
		t.Error("Expected isNew to be false when finding by phone")
	}

	if result.ID != "phone-id" {
		t.Errorf("Expected existing customer ID 'phone-id', got '%s'", result.ID)
	}

	if result.CPF != "98765432100" {
		t.Errorf("Expected CPF to be updated, got '%s'", result.CPF)
	}
	if result.Email != "pedro@example.com" {
		t.Errorf("Expected email to be updated, got '%s'", result.Email)
	}
}

func TestGetOrCreateCustomerUseCase_MergeOnlyNonEmpty(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	existingCustomer := &customer.Customer{
		ID:   "merge-id",
		Name: "Ana Merge",
		CPF:  "11122233344",
		RG:   "existing-rg",
	}
	repo.customers["merge-id"] = existingCustomer

	newCustomer := &customer.Customer{
		Name: "Ana Nova",
		CPF:  "11122233344",
		RG:   "",
	}

	result, isNew, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if isNew {
		t.Error("Expected isNew to be false")
	}

	if result.RG != "existing-rg" {
		t.Errorf("Expected RG to remain 'existing-rg', got '%s'", result.RG)
	}

	if result.Name != "Ana Nova" {
		t.Errorf("Expected name to be updated, got '%s'", result.Name)
	}
}

func TestGetOrCreateCustomerUseCase_CreateError(t *testing.T) {
	repo := newMockRepo()
	repo.createErr = errors.New("database error")

	uc := NewGetOrCreateCustomerUseCase(repo)

	newCustomer := &customer.Customer{
		Name: "Error Customer",
		CPF:  "99999999999",
	}

	_, _, err := uc.Execute(newCustomer)
	if err == nil {
		t.Error("Expected error when create fails")
	}
}

func TestGetOrCreateCustomerUseCase_UpdateError(t *testing.T) {
	repo := newMockRepo()

	existingCustomer := &customer.Customer{
		ID:  "update-err-id",
		CPF: "44455566677",
	}
	repo.customers["update-err-id"] = existingCustomer
	repo.updateErr = errors.New("update failed")

	uc := NewGetOrCreateCustomerUseCase(repo)

	newCustomer := &customer.Customer{
		Name: "Update Error",
		CPF:  "44455566677",
	}

	_, _, err := uc.Execute(newCustomer)
	if err == nil {
		t.Error("Expected error when update fails")
	}
}

func TestGetOrCreateCustomerUseCase_PreserveUserID(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetOrCreateCustomerUseCase(repo)

	existingCustomer := &customer.Customer{
		ID:     "user-id-test",
		CPF:    "77788899900",
		UserID: "original-user",
	}
	repo.customers["user-id-test"] = existingCustomer

	newCustomer := &customer.Customer{
		CPF:    "77788899900",
		UserID: "new-user",
	}

	result, _, err := uc.Execute(newCustomer)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.UserID != "new-user" {
		t.Errorf("Expected UserID 'new-user', got '%s'", result.UserID)
	}
}
