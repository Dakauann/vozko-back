package auth_usecase

import (
	"log"
	"strings"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/business_metrics"
	"vozko/domain/customer"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type adminRegisterUseCase struct {
	userRepo        user.UserRepository
	passwordService auth.PasswordService
	emailService    notification.EmailService
	docValidator    customer.DocumentValidator
	customerRepo    customer.CustomerRepository
	recordMetric    business_metrics.RecordMetricUseCase
	ensureDefaultWs workspace.EnsureDefaultWorkspaceUseCase
}

func NewAdminRegisterUseCase(
	userRepo user.UserRepository,
	passwordService auth.PasswordService,
	emailService notification.EmailService,
	docValidator customer.DocumentValidator,
	customerRepo customer.CustomerRepository,
	recordMetric business_metrics.RecordMetricUseCase,
	ensureDefaultWs workspace.EnsureDefaultWorkspaceUseCase,
) auth.AdminRegisterUseCase {
	return &adminRegisterUseCase{
		userRepo:        userRepo,
		passwordService: passwordService,
		emailService:    emailService,
		docValidator:    docValidator,
		customerRepo:    customerRepo,
		recordMetric:    recordMetric,
		ensureDefaultWs: ensureDefaultWs,
	}
}

func (uc *adminRegisterUseCase) Execute(input auth.CredentialsInput) (*auth.TokenPair, error) {
	existing, _ := uc.userRepo.FindByEmail(input.Email)
	if existing != nil {
		return nil, auth.ErrEmailAlreadyExists
	}

	hashedPassword, err := uc.passwordService.Hash(input.Password)
	if err != nil {
		return nil, err
	}

	normalizedType := strings.ToLower(strings.TrimSpace(input.CustomerType))
	if normalizedType == "" {
		return nil, auth.ErrInvalidCustomerType
	}

	var custType user.CustomerType
	var cpf, cnpj string

	switch normalizedType {
	case string(user.CustomerTypeIndividual):
		cpf = uc.docValidator.Normalize(strings.TrimSpace(input.CPF))
		if cpf == "" {
			return nil, auth.ErrMissingCustomerDocument
		}
		if len(cpf) != 11 || !uc.docValidator.ValidateCPFOrCNPJ(cpf) {
			return nil, auth.ErrInvalidCustomerDocument
		}
		custType = user.CustomerTypeIndividual
	case string(user.CustomerTypeCompany):
		cnpj = uc.docValidator.Normalize(strings.TrimSpace(input.CNPJ))
		if cnpj == "" {
			return nil, auth.ErrMissingCustomerDocument
		}
		if len(cnpj) != 14 || !uc.docValidator.ValidateCPFOrCNPJ(cnpj) {
			return nil, auth.ErrInvalidCustomerDocument
		}
		custType = user.CustomerTypeCompany
	default:
		return nil, auth.ErrInvalidCustomerType
	}

	sanitizedEmail := strings.ToLower(strings.TrimSpace(input.Email))

	u := &user.User{
		Username:      input.Name,
		Email:         sanitizedEmail,
		Password:      hashedPassword,
		Role:          user.RoleUser,
		CustomerType:  custType,
		CPF:           cpf,
		CNPJ:          cnpj,
		EmailVerified: false,
	}

	if err := uc.userRepo.Create(u); err != nil {
		return nil, err
	}

	uc.recordUserCreation(u.ID, u.Email, string(custType))

	// Provision the user's default workspace, same as the self-service register
	// flow. Without this, admin-created accounts have no workspace at all, which
	// leaves them unable to load the app or see/accept pending invites.
	if uc.ensureDefaultWs != nil {
		if _, wsErr := uc.ensureDefaultWs.Execute(u.ID, u.Email, strings.TrimSpace(input.ReferralCode)); wsErr != nil {
			log.Printf("[admin-register] failed to ensure default workspace for user %s: %v", u.ID, wsErr)
		}
	}

	go uc.linkExistingCustomer(u.ID, cpf, cnpj)
	go uc.sendWelcomeEmail(u.Email)

	return &auth.TokenPair{
		UserID:       u.ID,
		Email:        u.Email,
		Name:         u.Username,
		Role:         string(u.Role),
		CustomerType: string(u.CustomerType),
	}, nil
}

func (uc *adminRegisterUseCase) linkExistingCustomer(userID, cpf, cnpj string) {
	var customerDoc string
	if cpf != "" {
		customerDoc = cpf
	} else if cnpj != "" {
		customerDoc = cnpj
	}

	if customerDoc != "" {
		cust, err := uc.customerRepo.GetCustomerByDocument(customerDoc)
		if err == nil && cust != nil && cust.UserID == "" {
			cust.UserID = userID
			_ = uc.customerRepo.UpdateCustomer(cust)
		}
	}
}

func (uc *adminRegisterUseCase) sendWelcomeEmail(email string) {
	subject := "Bem-vindo à " + brand.Active().Name + "!"
	_ = uc.emailService.SendTemplate(email, subject, "welcome_email.html", map[string]interface{}{
		"Email": email,
	})
}

func (uc *adminRegisterUseCase) recordUserCreation(userID, email, customerType string) {
	if uc.recordMetric == nil {
		return
	}

	err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventUserAccountCreated,
		EntityID:   userID,
		EntityType: business_metrics.EntityTypeUser,
		UserID:     &userID,
		Metadata: map[string]string{
			"email":         email,
			"customer_type": customerType,
		},
	})

	if err != nil {
		log.Printf("failed to record user account created metric: %v", err)
	}
}
