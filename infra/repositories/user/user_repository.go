package user_repository

import (
	"errors"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

func encryptCPF(plain string) (piigorm.EncryptedString, piigorm.BlindIndex, error) {
	return encryptDoc(schema.UserCPFBlindScope, plain)
}

func encryptCNPJ(plain string) (piigorm.EncryptedString, piigorm.BlindIndex, error) {
	return encryptDoc(schema.UserCNPJBlindScope, plain)
}

func encryptDoc(scope, plain string) (piigorm.EncryptedString, piigorm.BlindIndex, error) {
	if plain == "" {
		return piigorm.Null(), nil, nil
	}
	bi, err := piigorm.NewBlindIndex(scope, plain)
	if err != nil {
		return piigorm.EncryptedString{}, nil, err
	}
	return piigorm.NewEncrypted(plain), bi, nil
}

type UserRepositoryImpl struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) user.UserRepository {
	return &UserRepositoryImpl{db: db}
}

func (r *UserRepositoryImpl) WithTx(tx interface{}) user.UserRepository {
	return &UserRepositoryImpl{db: tx.(*gorm.DB)}
}

func (r *UserRepositoryImpl) FindByIDs(userIds []string) ([]*user.User, error) {
	var dbUsers []schema.User
	if err := r.db.Where("id IN ?", userIds).Find(&dbUsers).Error; err != nil {
		return nil, err
	}
	users := make([]*user.User, len(dbUsers))
	for i := range dbUsers {
		users[i] = mapToDomain(&dbUsers[i])
	}
	return users, nil
}

func (r *UserRepositoryImpl) Create(u *user.User) error {
	cpfEnc, cpfBlind, err := encryptCPF(u.CPF)
	if err != nil {
		return err
	}
	cnpjEnc, cnpjBlind, err := encryptCNPJ(u.CNPJ)
	if err != nil {
		return err
	}
	dbUser := &schema.User{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		Password:      u.Password,
		Picture:       u.Picture,
		Role:          string(u.Role),
		CustomerType:  string(u.CustomerType),
		CPF:           cpfEnc,
		CPFBlind:      cpfBlind,
		CNPJ:          cnpjEnc,
		CNPJBlind:     cnpjBlind,
		EmailVerified: u.EmailVerified,
	}
	if err := r.db.Create(dbUser).Error; err != nil {
		return err
	}
	u.ID = dbUser.ID
	if u.CustomerType == "" {
		u.CustomerType = user.CustomerType(dbUser.CustomerType)
	}
	u.CPF = dbUser.CPF.String()
	u.CNPJ = dbUser.CNPJ.String()
	u.CreatedAt = dbUser.CreatedAt
	u.UpdatedAt = dbUser.UpdatedAt
	return nil
}

func (r *UserRepositoryImpl) FindByEmail(email string) (*user.User, error) {
	var dbUser schema.User
	if err := r.db.Where("email = ?", email).First(&dbUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, user.ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbUser), nil
}

func (r *UserRepositoryImpl) FindByDocument(doc string) (*user.User, error) {
	if doc == "" {
		return nil, user.ErrNotFound
	}
	cpfBlind, err := piigorm.NewBlindIndex(schema.UserCPFBlindScope, doc)
	if err != nil {
		return nil, err
	}

	cnpjBlind, _ := piigorm.NewBlindIndex(schema.UserCNPJBlindScope, doc)
	var dbUser schema.User
	if err := r.db.Where("cpf_blind = ? OR cnpj_blind = ?", []byte(cpfBlind), []byte(cnpjBlind)).First(&dbUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, user.ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbUser), nil
}

func (r *UserRepositoryImpl) FindByID(id string) (*user.User, error) {
	var dbUser schema.User
	if err := r.db.Where("id = ?", id).First(&dbUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, user.ErrNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbUser), nil
}

func (r *UserRepositoryImpl) Update(userId string, u *user.User) error {
	cpfEnc, cpfBlind, err := encryptCPF(u.CPF)
	if err != nil {
		return err
	}
	cnpjEnc, cnpjBlind, err := encryptCNPJ(u.CNPJ)
	if err != nil {
		return err
	}

	updates := map[string]any{
		"username":       u.Username,
		"email":          u.Email,
		"password":       u.Password,
		"picture":        u.Picture,
		"role":           string(u.Role),
		"customer_type":  string(u.CustomerType),
		"cpf":            cpfEnc,
		"cpf_blind":      cpfBlind,
		"cnpj":           cnpjEnc,
		"cnpj_blind":     cnpjBlind,
		"email_verified": u.EmailVerified,
	}
	return r.db.Model(&schema.User{}).Where("id = ?", userId).Updates(updates).Error
}

func (r *UserRepositoryImpl) Delete(userId string) error {
	return r.db.Delete(&schema.User{}, "id = ?", userId).Error
}

func (r *UserRepositoryImpl) GetUserRole(userId string) (string, error) {
	var role string
	err := r.db.Model(&schema.User{}).Where("id = ?", userId).Pluck("role", &role).Error
	if err != nil {
		return "", err
	}
	if role == "" {
		return string(user.RoleUser), nil
	}
	return role, nil
}

func (r *UserRepositoryImpl) List(input user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	pagination := shared.NormalizePagination(input.Pagination)
	query := r.db.Model(&schema.User{})

	if input.Search != "" {
		searchPattern := "%" + strings.ToLower(input.Search) + "%"
		query = query.Where("LOWER(username) LIKE ? OR LOWER(email) LIKE ?", searchPattern, searchPattern)
	}

	if input.Role != nil {
		query = query.Where("role = ?", string(*input.Role))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbUsers []schema.User
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbUsers).Error; err != nil {
		return nil, err
	}

	users := make([]*user.User, len(dbUsers))
	for i := range dbUsers {
		users[i] = mapToDomain(&dbUsers[i])
	}

	return shared.NewPaginatedResult(users, pagination, total), nil
}

func (r *UserRepositoryImpl) CountByRole(role user.Role) (int64, error) {
	var count int64
	if err := r.db.Model(&schema.User{}).Where("role = ?", string(role)).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *UserRepositoryImpl) GetTokenVersion(userId string) (int, error) {
	var version int
	err := r.db.Model(&schema.User{}).Where("id = ?", userId).Pluck("token_version", &version).Error
	return version, err
}

func (r *UserRepositoryImpl) IncrementTokenVersion(userId string) (int, error) {
	result := r.db.Model(&schema.User{}).Where("id = ?", userId).
		Update("token_version", gorm.Expr("token_version + 1"))
	if result.Error != nil {
		return 0, result.Error
	}
	return r.GetTokenVersion(userId)
}

func mapToDomain(dbUser *schema.User) *user.User {
	return &user.User{
		ID:            dbUser.ID,
		Username:      dbUser.Username,
		Email:         dbUser.Email,
		Password:      dbUser.Password,
		Picture:       dbUser.Picture,
		Role:          user.Role(dbUser.Role),
		CustomerType:  user.CustomerType(dbUser.CustomerType),
		CPF:           dbUser.CPF.String(),
		CNPJ:          dbUser.CNPJ.String(),
		EmailVerified: dbUser.EmailVerified,
		TokenVersion:  dbUser.TokenVersion,
		DisabledAt:    dbUser.DisabledAt,
		CreatedAt:     dbUser.CreatedAt,
		UpdatedAt:     dbUser.UpdatedAt,
	}
}
