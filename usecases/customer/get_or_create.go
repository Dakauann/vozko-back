package customer_usecase

import (
	"vozko/domain/customer"

	"github.com/google/uuid"
)

type getOrCreateCustomerUseCase struct {
	repo customer.CustomerRepository
}

func NewGetOrCreateCustomerUseCase(repo customer.CustomerRepository) customer.GetOrCreateCustomerUseCase {
	return &getOrCreateCustomerUseCase{repo: repo}
}

func (uc *getOrCreateCustomerUseCase) Execute(c *customer.Customer) (*customer.Customer, bool, error) {

	if c.CPF != "" {
		existing, err := uc.repo.GetCustomerByDocument(c.CPF)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {

			updated := uc.mergeCustomer(existing, c)
			if err := uc.repo.UpdateCustomer(updated); err != nil {
				return nil, false, err
			}
			return updated, false, nil
		}
	}

	if c.Email != "" {
		existing, err := uc.repo.GetCustomerByEmail(c.Email)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {
			updated := uc.mergeCustomer(existing, c)
			if err := uc.repo.UpdateCustomer(updated); err != nil {
				return nil, false, err
			}
			return updated, false, nil
		}
	}

	if c.MobileNumber != "" {
		existing, err := uc.repo.GetCustomerByPhone(c.MobileNumber)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {
			updated := uc.mergeCustomer(existing, c)
			if err := uc.repo.UpdateCustomer(updated); err != nil {
				return nil, false, err
			}
			return updated, false, nil
		}
	}

	if c.ID == "" {
		c.ID = uuid.New().String()
	}

	if err := uc.repo.CreateCustomer(c); err != nil {
		return nil, false, err
	}

	return c, true, nil
}

func (uc *getOrCreateCustomerUseCase) mergeCustomer(existing, new *customer.Customer) *customer.Customer {
	result := *existing

	if new.Name != "" {
		result.Name = new.Name
	}
	if new.Email != "" {
		result.Email = new.Email
	}
	if new.MobileNumber != "" {
		result.MobileNumber = new.MobileNumber
	}
	if new.CPF != "" {
		result.CPF = new.CPF
	}
	if new.RG != "" {
		result.RG = new.RG
	}
	if new.Gender != "" {
		result.Gender = new.Gender
	}
	if new.BirthDate != "" {
		result.BirthDate = new.BirthDate
	}
	if new.Nationality != "" {
		result.Nationality = new.Nationality
	}
	if new.UserID != "" {
		result.UserID = new.UserID
	}

	return &result
}
