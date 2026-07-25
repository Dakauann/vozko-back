package payment_usecase

import (
	"strings"

	"vozko/domain/payment"
)

type paymentSplitService struct {
	repo payment.PaymentSplitRepository
}

func newPaymentSplitService(repo payment.PaymentSplitRepository) *paymentSplitService {
	return &paymentSplitService{repo: repo}
}

func (s *paymentSplitService) normalizeAndValidate(split *payment.PaymentSplit) error {
	normalizedType, err := payment.NormalizeSplitType(string(split.Type))
	if err != nil {
		return err
	}
	split.Type = normalizedType

	provider, err := payment.NormalizeSplitProvider(string(split.Provider))
	if err != nil {
		return err
	}
	split.Provider = provider

	return payment.ValidateSplit(split)
}

type createPaymentSplitUseCase struct {
	service *paymentSplitService
}

func NewCreatePaymentSplitUseCase(repo payment.PaymentSplitRepository) payment.CreatePaymentSplitUseCase {
	return &createPaymentSplitUseCase{service: newPaymentSplitService(repo)}
}

func (uc *createPaymentSplitUseCase) Execute(split *payment.PaymentSplit) (*payment.PaymentSplit, error) {
	if split == nil {
		return nil, payment.ErrPaymentSplitNameRequired
	}

	split.Name = strings.TrimSpace(split.Name)
	if err := uc.service.normalizeAndValidate(split); err != nil {
		return nil, err
	}

	if err := uc.service.repo.Create(split); err != nil {
		return nil, err
	}

	return uc.service.repo.GetByID(split.ID)
}

type updatePaymentSplitUseCase struct {
	service *paymentSplitService
}

func NewUpdatePaymentSplitUseCase(repo payment.PaymentSplitRepository) payment.UpdatePaymentSplitUseCase {
	return &updatePaymentSplitUseCase{service: newPaymentSplitService(repo)}
}

type getPaymentSplitSuppliersUseCase struct {
	repo payment.PaymentSplitRepository
}

func NewGetPaymentSplitSuppliersUseCase(repo payment.PaymentSplitRepository) payment.GetPaymentSplitSuppliersUseCase {
	return &getPaymentSplitSuppliersUseCase{repo: repo}
}

func (uc *getPaymentSplitSuppliersUseCase) Execute() ([]*payment.PaymentSplit, error) {
	return uc.repo.GetSuppliers()
}

func (uc *updatePaymentSplitUseCase) Execute(id string, split *payment.PaymentSplit) (*payment.PaymentSplit, error) {
	if id == "" {
		return nil, payment.ErrPaymentSplitNotFound
	}
	existing, err := uc.service.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	existing.Name = strings.TrimSpace(split.Name)
	existing.Type = split.Type
	existing.Provider = split.Provider
	existing.WalletID = split.WalletID
	existing.Percentage = split.Percentage
	existing.FixedAmount = split.FixedAmount

	if err := uc.service.normalizeAndValidate(existing); err != nil {
		return nil, err
	}

	if err := uc.service.repo.Update(existing); err != nil {
		return nil, err
	}

	return uc.service.repo.GetByID(id)
}

type deletePaymentSplitUseCase struct {
	repo payment.PaymentSplitRepository
}

func NewDeletePaymentSplitUseCase(repo payment.PaymentSplitRepository) payment.DeletePaymentSplitUseCase {
	return &deletePaymentSplitUseCase{repo: repo}
}

func (uc *deletePaymentSplitUseCase) Execute(id string) error {
	if id == "" {
		return payment.ErrPaymentSplitNotFound
	}
	return uc.repo.Delete(id)
}

type getPaymentSplitUseCase struct {
	repo payment.PaymentSplitRepository
}

func NewGetPaymentSplitUseCase(repo payment.PaymentSplitRepository) payment.GetPaymentSplitUseCase {
	return &getPaymentSplitUseCase{repo: repo}
}

func (uc *getPaymentSplitUseCase) Execute(id string) (*payment.PaymentSplit, error) {
	if id == "" {
		return nil, payment.ErrPaymentSplitNotFound
	}
	return uc.repo.GetByID(id)
}

type listPaymentSplitsUseCase struct {
	repo payment.PaymentSplitRepository
}

func NewListPaymentSplitsUseCase(repo payment.PaymentSplitRepository) payment.ListPaymentSplitsUseCase {
	return &listPaymentSplitsUseCase{repo: repo}
}

func (uc *listPaymentSplitsUseCase) Execute() ([]payment.PaymentSplit, error) {
	return uc.repo.List()
}
