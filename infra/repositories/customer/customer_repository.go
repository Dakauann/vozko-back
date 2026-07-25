package customer_repository

import (
	"database/sql"
	"errors"

	"vozko/domain/customer"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type customerRepository struct {
	db *gorm.DB
}

func NewCustomerRepository(db *gorm.DB) customer.CustomerRepository {
	return &customerRepository{db: db}
}

func encryptCustomerDoc(scope, plain string) (piigorm.EncryptedString, piigorm.BlindIndex, error) {
	if plain == "" {
		return piigorm.Null(), nil, nil
	}
	bi, err := piigorm.NewBlindIndex(scope, plain)
	if err != nil {
		return piigorm.EncryptedString{}, nil, err
	}
	return piigorm.NewEncrypted(plain), bi, nil
}

func buildSchemaCustomer(cust *customer.Customer) (*schema.Customer, error) {
	cpfEnc, cpfBlind, err := encryptCustomerDoc(schema.CustomerCPFBlindScope, cust.CPF)
	if err != nil {
		return nil, err
	}
	cnpjEnc, cnpjBlind, err := encryptCustomerDoc(schema.CustomerCNPJBlindScope, cust.CNPJ)
	if err != nil {
		return nil, err
	}
	return &schema.Customer{
		ID:           cust.ID,
		UserID:       toNullString(cust.UserID),
		Name:         cust.Name,
		Email:        cust.Email,
		Gender:       cust.Gender,
		BirthDate:    cust.BirthDate,
		MobileNumber: cust.MobileNumber,
		CPF:          cpfEnc,
		CPFBlind:     cpfBlind,
		RG:           cust.RG,
		Nationality:  cust.Nationality,
		CNPJ:         cnpjEnc,
		CNPJBlind:    cnpjBlind,
	}, nil
}

func mapCustomerToDomain(dbModel *schema.Customer) *customer.Customer {
	return &customer.Customer{
		ID:           dbModel.ID,
		UserID:       fromNullString(dbModel.UserID),
		Name:         dbModel.Name,
		Email:        dbModel.Email,
		Gender:       dbModel.Gender,
		BirthDate:    dbModel.BirthDate,
		MobileNumber: dbModel.MobileNumber,
		CPF:          dbModel.CPF.String(),
		RG:           dbModel.RG,
		Nationality:  dbModel.Nationality,
		CNPJ:         dbModel.CNPJ.String(),
	}
}

func (r *customerRepository) CreateCustomer(cust *customer.Customer) error {
	dbModel, err := buildSchemaCustomer(cust)
	if err != nil {
		return err
	}
	if err := r.db.Create(dbModel).Error; err != nil {
		return err
	}
	cust.ID = dbModel.ID
	return nil
}

func (r *customerRepository) GetCustomerByID(customerID string) (*customer.Customer, error) {
	var dbModel schema.Customer
	if err := r.db.Where("id = ?", customerID).First(&dbModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapCustomerToDomain(&dbModel), nil
}

func (r *customerRepository) GetCustomerByDocument(cpfCnpj string) (*customer.Customer, error) {
	if cpfCnpj == "" {
		return nil, nil
	}
	cpfBlind, err := piigorm.NewBlindIndex(schema.CustomerCPFBlindScope, cpfCnpj)
	if err != nil {
		return nil, err
	}
	cnpjBlind, _ := piigorm.NewBlindIndex(schema.CustomerCNPJBlindScope, cpfCnpj)

	var dbModel schema.Customer
	if err := r.db.Where("cpf_blind = ? OR cnpj_blind = ?", []byte(cpfBlind), []byte(cnpjBlind)).First(&dbModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapCustomerToDomain(&dbModel), nil
}

func (r *customerRepository) GetCustomerByDocumentEmailOrPhone(cpfCnpj, email, phone string) (*customer.Customer, error) {
	if cpfCnpj == "" && email == "" && phone == "" {
		return nil, nil
	}
	query := r.db.Model(&schema.Customer{})
	hasCondition := false
	if cpfCnpj != "" {
		cpfBlind, err := piigorm.NewBlindIndex(schema.CustomerCPFBlindScope, cpfCnpj)
		if err != nil {
			return nil, err
		}
		cnpjBlind, _ := piigorm.NewBlindIndex(schema.CustomerCNPJBlindScope, cpfCnpj)
		query = query.Where("cpf_blind = ? OR cnpj_blind = ?", []byte(cpfBlind), []byte(cnpjBlind))
		hasCondition = true
	}
	if email != "" {
		if hasCondition {
			query = query.Or("email = ?", email)
		} else {
			query = query.Where("email = ?", email)
			hasCondition = true
		}
	}
	if phone != "" {
		if hasCondition {
			query = query.Or("mobile_number = ?", phone)
		} else {
			query = query.Where("mobile_number = ?", phone)
		}
	}

	var dbModel schema.Customer
	if err := query.First(&dbModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapCustomerToDomain(&dbModel), nil
}

func (r *customerRepository) GetCustomerByEmail(email string) (*customer.Customer, error) {
	var dbModel schema.Customer
	if err := r.db.Where("email = ?", email).First(&dbModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapCustomerToDomain(&dbModel), nil
}

func (r *customerRepository) GetCustomerByPhone(phone string) (*customer.Customer, error) {
	var dbModel schema.Customer
	if err := r.db.Where("mobile_number = ?", phone).First(&dbModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return mapCustomerToDomain(&dbModel), nil
}

func (r *customerRepository) UpdateCustomer(cust *customer.Customer) error {
	dbModel, err := buildSchemaCustomer(cust)
	if err != nil {
		return err
	}
	return r.db.Save(dbModel).Error
}

func (r *customerRepository) ListCustomersByUser(userID string) ([]customer.Customer, error) {
	var dbModels []schema.Customer
	if err := r.db.Where("user_id = ?", userID).Find(&dbModels).Error; err != nil {
		return nil, err
	}
	customers := make([]customer.Customer, 0, len(dbModels))
	for i := range dbModels {
		customers = append(customers, *mapCustomerToDomain(&dbModels[i]))
	}
	return customers, nil
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func fromNullString(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}
