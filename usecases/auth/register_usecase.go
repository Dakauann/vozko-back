package auth_usecase

import (
	"log"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/auth"
	"vozko/domain/business_metrics"
	"vozko/domain/customer"
	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
	"vozko/infra/geolocation"
)

type registerUseCase struct {
	userRepo        user.UserRepository
	passwordService auth.PasswordService
	tokenIssuer     auth.TokenIssuer
	sessionRepo     auth.SessionRepository
	emailService    notification.EmailService
	docValidator    customer.DocumentValidator
	verifyToken     auth.VerifyEmailTokenUseCase
	tokenRepo       auth.EmailVerificationRepository
	customerRepo    customer.CustomerRepository
	recordMetric    business_metrics.RecordMetricUseCase
	ensureDefaultWs workspace.EnsureDefaultWorkspaceUseCase
}

func NewRegisterUseCase(
	userRepo user.UserRepository,
	passwordService auth.PasswordService,
	tokenIssuer auth.TokenIssuer,
	sessionRepo auth.SessionRepository,
	emailService notification.EmailService,
	docValidator customer.DocumentValidator,
	verifyToken auth.VerifyEmailTokenUseCase,
	tokenRepo auth.EmailVerificationRepository,
	customerRepo customer.CustomerRepository,
	recordMetric business_metrics.RecordMetricUseCase,
	ensureDefaultWs workspace.EnsureDefaultWorkspaceUseCase,
) auth.RegisterUseCase {
	return &registerUseCase{
		userRepo:        userRepo,
		passwordService: passwordService,
		tokenIssuer:     tokenIssuer,
		sessionRepo:     sessionRepo,
		emailService:    emailService,
		docValidator:    docValidator,
		verifyToken:     verifyToken,
		tokenRepo:       tokenRepo,
		customerRepo:    customerRepo,
		recordMetric:    recordMetric,
		ensureDefaultWs: ensureDefaultWs,
	}
}

func (uc *registerUseCase) Execute(input auth.CredentialsInput) (*auth.TokenPair, error) {

	if err := uc.verifyToken.Execute(input.VerificationToken); err != nil {
		return nil, err
	}

	if !isStrongPassword(input.Password) {
		return nil, auth.ErrWeakPassword
	}

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
		if existing, _ := uc.userRepo.FindByDocument(cpf); existing != nil {
			return nil, auth.ErrDocumentAlreadyExists
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
		if existing, _ := uc.userRepo.FindByDocument(cnpj); existing != nil {
			return nil, auth.ErrDocumentAlreadyExists
		}
		custType = user.CustomerTypeCompany
	default:
		return nil, auth.ErrInvalidCustomerType
	}

	sanatizedEmail := strings.ToLower(strings.TrimSpace(input.Email))

	u := &user.User{
		Username:      input.Name,
		Email:         sanatizedEmail,
		Password:      hashedPassword,
		Role:          user.RoleUser,
		CustomerType:  custType,
		CPF:           cpf,
		CNPJ:          cnpj,
		EmailVerified: true,
	}

	if err := uc.userRepo.Create(u); err != nil {
		return nil, err
	}

	uc.recordUserCreation(u.ID, u.Email, string(custType))

	if uc.ensureDefaultWs != nil {
		if _, wsErr := uc.ensureDefaultWs.Execute(u.ID, u.Email, strings.TrimSpace(input.ReferralCode)); wsErr != nil {
			log.Printf("[register] failed to ensure default workspace for user %s: %v", u.ID, wsErr)
		}
	}

	go uc.linkExistingCustomer(u.ID, cpf, cnpj)

	tokens, err := uc.tokenIssuer.Issue(u)
	if err != nil {
		return nil, err
	}

	rawRefresh, hashRefresh, err := uc.tokenIssuer.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	session := &auth.Session{
		UserID:           u.ID,
		RefreshTokenHash: hashRefresh,
		AccessJTI:        tokens.AccessJTI,
		DeviceInfo:       input.DeviceInfo,
		IPAddress:        input.IPAddress,
		ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
	}

	if input.IPAddress != "" {
		session.Location = geolocation.LookupIP(input.IPAddress)
	}

	if err := uc.sessionRepo.Create(session); err != nil {
		return nil, err
	}

	tokens.RefreshToken = rawRefresh

	_ = uc.tokenRepo.MarkAsUsed(input.VerificationToken)

	go uc.sendWelcomeEmail(u.Email)

	return tokens, nil
}

func (uc *registerUseCase) linkExistingCustomer(userID, cpf, cnpj string) {
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

func (uc *registerUseCase) sendWelcomeEmail(email string) {
	subject := "Bem-vindo à " + brand.Active().Name + "!"
	_ = uc.emailService.SendTemplate(email, subject, "welcome_email.html", map[string]interface{}{
		"Email": email,
	})
}

func (uc *registerUseCase) recordUserCreation(userID, email, customerType string) {
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
