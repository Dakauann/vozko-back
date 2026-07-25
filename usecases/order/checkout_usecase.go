package order_usecase

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"vozko/brand"
	"vozko/domain/address"
	"vozko/domain/cart"
	"vozko/domain/customer"
	"vozko/domain/notification"
	"vozko/domain/order"
	"vozko/domain/payment"
	"vozko/domain/product"
	"vozko/domain/user"
	asaas_service "vozko/infra/asaas"
)

type checkoutUseCase struct {
	userRepo         user.UserRepository
	orderRepo        order.OrderRepository
	cartRepo         cart.CartRepository
	addressRepo      address.AddressRepository
	productRepo      product.ProductRepository
	paymentRepo      payment.PaymentRepository
	paymentSplitRepo payment.PaymentSplitRepository
	pricingService   payment.PricingService
	docValidator     customer.DocumentValidator
	asaasService     asaas_service.AsaasServiceUseCases
	emailService     notification.EmailService
}

func NewCheckoutUseCase(
	orderRepo order.OrderRepository,
	cartRepo cart.CartRepository,
	addressRepo address.AddressRepository,
	productRepo product.ProductRepository,
	paymentRepo payment.PaymentRepository,
	paymentSplitRepo payment.PaymentSplitRepository,
	docValidator customer.DocumentValidator,
	asaasService asaas_service.AsaasServiceUseCases,
	emailService notification.EmailService,
	userRepo user.UserRepository,
	pricingService payment.PricingService) order.CheckoutUseCase {
	return &checkoutUseCase{
		orderRepo:        orderRepo,
		cartRepo:         cartRepo,
		addressRepo:      addressRepo,
		productRepo:      productRepo,
		paymentRepo:      paymentRepo,
		paymentSplitRepo: paymentSplitRepo,
		docValidator:     docValidator,
		asaasService:     asaasService,
		emailService:     emailService,
		pricingService:   pricingService,
		userRepo:         userRepo,
	}
}

type checkoutItem struct {
	ProductID       string
	VariantID       string
	Quantity        int
	SelectedOptions []order.OrderItemOption
	CartItemID      string
}

func (uc *checkoutUseCase) Execute(userID string, request *order.CheckoutRequest) (*order.Order, error) {
	dbUser, err := uc.userRepo.FindByID(userID)

	if err != nil {
		return nil, err
	}

	_, err = uc.addressRepo.GetByID(userID, request.AddressID)
	if err != nil {
		return nil, order.ErrAddressNotFound
	}

	sanitizedDocument := request.CustomerDocument
	if uc.docValidator != nil {
		if !uc.docValidator.ValidateCPFOrCNPJ(request.CustomerDocument) {
			return nil, order.ErrInvalidCustomerDocument
		}
		sanitizedDocument = uc.docValidator.Normalize(request.CustomerDocument)
	}

	var itemsToCheckout []checkoutItem
	var cartItemIDsToRemove []string

	if request.DirectItem != nil {

		if request.DirectItem.ProductID == "" || request.DirectItem.VariantID == "" {
			return nil, order.ErrInvalidOrderData
		}
		if request.DirectItem.Quantity <= 0 {
			request.DirectItem.Quantity = 1
		}
		itemsToCheckout = []checkoutItem{{
			ProductID:       request.DirectItem.ProductID,
			VariantID:       request.DirectItem.VariantID,
			Quantity:        request.DirectItem.Quantity,
			SelectedOptions: request.DirectItem.Options,
		}}
	} else {

		userCart, err := uc.cartRepo.GetCartByUserID(userID)
		if err != nil {
			return nil, err
		}

		if userCart == nil || len(userCart.Items) == 0 {
			return nil, order.ErrCartEmpty
		}

		if len(request.CartItemIDs) > 0 {

			cartItems, err := uc.cartRepo.GetCartItemsByIDs(userID, request.CartItemIDs)
			if err != nil {
				return nil, err
			}
			if len(cartItems) == 0 {
				return nil, order.ErrCartEmpty
			}
			for _, item := range cartItems {
				var options []order.OrderItemOption
				for _, opt := range item.SelectedOptions {
					options = append(options, order.OrderItemOption{
						OptionType:  opt.OptionType,
						OptionValue: opt.OptionValue,
					})
				}
				itemsToCheckout = append(itemsToCheckout, checkoutItem{
					ProductID: item.ProductID,
					VariantID: item.VariantID,
					Quantity:  item.Quantity,

					SelectedOptions: options,
					CartItemID:      item.ID,
				})
				cartItemIDsToRemove = append(cartItemIDsToRemove, item.ID)
			}
		} else {

			for _, item := range userCart.Items {
				var options []order.OrderItemOption
				for _, opt := range item.SelectedOptions {
					options = append(options, order.OrderItemOption{
						OptionType:  opt.OptionType,
						OptionValue: opt.OptionValue,
					})
				}
				itemsToCheckout = append(itemsToCheckout, checkoutItem{
					ProductID: item.ProductID,
					VariantID: item.VariantID,
					Quantity:  item.Quantity,

					SelectedOptions: options,
					CartItemID:      item.ID,
				})
				cartItemIDsToRemove = append(cartItemIDsToRemove, item.ID)
			}
		}
	}

	if len(itemsToCheckout) == 0 {
		return nil, order.ErrCartEmpty
	}

	productIDs := make(map[string]bool)
	for _, item := range itemsToCheckout {
		if item.ProductID != "" {
			productIDs[item.ProductID] = true
		}
	}

	var productIDsList []string
	for productID := range productIDs {
		productIDsList = append(productIDsList, productID)
	}

	products, err := uc.productRepo.FindByIDs(productIDsList)
	if err != nil {
		return nil, err
	}

	productMap := make(map[string]*product.Product)
	for _, p := range products {
		productMap[p.ID] = p
	}

	var orderItems []order.OrderItem
	var grossAmount float64

	ownersSplits, err := uc.paymentSplitRepo.GetByType(payment.SplitTypeOwner)
	if err != nil {
		return nil, err
	}

	ownerPercentages := make(map[string]float64)
	ownerValues := make(map[string]float64)

	ownerSplitsByID := make(map[string]*payment.PaymentSplit)
	for _, osplit := range ownersSplits {
		ownerPercentages[osplit.ID] = osplit.Percentage
		ownerSplitsByID[osplit.ID] = osplit
	}

	for _, checkoutItem := range itemsToCheckout {
		if checkoutItem.ProductID == "" || checkoutItem.VariantID == "" {
			continue
		}

		prod, exists := productMap[checkoutItem.ProductID]
		if !exists {
			return nil, order.ErrProductNotFound
		}

		var variant *product.Variant
		for i := range prod.Variants {
			if prod.Variants[i].ID == checkoutItem.VariantID {
				variant = &prod.Variants[i]
				break
			}
		}

		if variant == nil {
			return nil, order.ErrVariantNotFound
		}

		if !variant.Announced {
			return nil, order.ErrVariantNotAnnounced
		}

		if variant.Inventory < checkoutItem.Quantity {
			return nil, order.ErrInsufficientStock
		}

		unitPrice, err := uc.pricingService.CalculateUnitPrice(dbUser, variant, checkoutItem.Quantity)
		if err != nil {
			return nil, err
		}
		itemTotal := float64(checkoutItem.Quantity) * unitPrice
		grossAmount += itemTotal

		orderItems = append(orderItems, order.OrderItem{
			ProductID:  prod.ID,
			VariantID:  variant.ID,
			SKU:        variant.SKU,
			Quantity:   checkoutItem.Quantity,
			UnitPrice:  unitPrice,
			TotalPrice: itemTotal,
			Options:    checkoutItem.SelectedOptions,
		})
	}

	ownerValues = uc.computeOwnerAllocations(ownerPercentages, make(map[string]float64), grossAmount)

	if len(orderItems) == 0 {
		return nil, order.ErrCartEmpty
	}

	now := time.Now()

	paymentExpiryMinutes := 30
	expiresAt := now.Add(time.Duration(paymentExpiryMinutes) * time.Minute)

	newOrder := &order.Order{
		ID:                uuid.NewString(),
		UserID:            userID,
		ShippingAddressID: request.AddressID,
		CustomerName:      request.CustomerName,
		CustomerDocument:  sanitizedDocument,
		Items:             orderItems,
		TotalAmount:       grossAmount,
		Status:            order.StatusPending,
		ExpiresAt:         &expiresAt,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	inventoryUpdates := make([]order.InventoryUpdate, 0)
	for _, item := range orderItems {
		inventoryUpdates = append(inventoryUpdates, order.InventoryUpdate{
			VariantID: item.VariantID,
			Quantity:  -item.Quantity,
		})
	}

	if err := uc.orderRepo.CreateWithInventoryReservation(newOrder, inventoryUpdates); err != nil {
		return nil, err
	}

	if len(cartItemIDsToRemove) > 0 {
		if err := uc.cartRepo.RemoveCartItems(userID, cartItemIDsToRemove); err != nil {

		}
	}

	dueDate := expiresAt.Format("2006-01-02")
	paymentID := uuid.NewString()

	paymentSplits := make([]asaas_service.AsaasSplit, 0)

	for ownerSplitID, amount := range ownerValues {
		split, exists := ownerSplitsByID[ownerSplitID]
		if !exists || split == nil {
			return nil, fmt.Errorf("payment split not found for id: %s", ownerSplitID)
		}

		paymentSplits = append(paymentSplits, asaas_service.AsaasSplit{
			WalletID:   split.WalletID,
			FixedValue: amount,
		})
	}

	ownerSplitCount := 0
	for i := range paymentSplits {
		for _, osplit := range ownersSplits {
			if paymentSplits[i].WalletID == osplit.WalletID {
				ownerSplitCount++
				if ownerSplitCount == 1 || ownerSplitCount == 2 {
					paymentSplits[i].FixedValue -= 3.0
					break
				}
			}
		}
		if ownerSplitCount >= 2 {
			break
		}
	}

	asaasPayment := &asaas_service.AsaasPayment{
		BillingType:       "PIX",
		Value:             grossAmount,
		DueDate:           dueDate,
		Description:       "Pagamento de pedido",
		ExternalReference: paymentID,
		Split:             paymentSplits,
	}

	createdPayment, err := uc.asaasService.CreatePayment(request.CustomerName, sanitizedDocument, asaasPayment)
	if err != nil {
		return nil, err
	}

	pixQrCode, pixCopy, err := uc.asaasService.GetPaymentQrCode(createdPayment.ID)
	if err != nil {
		return nil, err
	}

	domainPayment := &payment.Payment{
		ID:          paymentID,
		UserID:      userID,
		OrderID:     &newOrder.ID,
		Amount:      grossAmount,
		Status:      payment.StatusPending,
		BillingType: payment.BillingTypePix,
		ExternalID:  createdPayment.ID,
		PixQrCode:   &pixQrCode,
		PixCopy:     &pixCopy,
		CrearedAt:   now.Unix(),
		UpdatedAt:   now.Unix(),
		DueDate:     expiresAt.Unix(),
	}

	err = uc.paymentRepo.CreatePayment(domainPayment)
	if err != nil {
		return nil, err
	}

	orderResult, err := uc.orderRepo.GetByID(userID, newOrder.ID)
	if err != nil {
		return nil, err
	}

	go uc.sendOrderConfirmationEmail(orderResult, request.CustomerName)

	return orderResult, nil
}

func (uc *checkoutUseCase) sendOrderConfirmationEmail(order *order.Order, customerName string) {
	subject := "Pedido Confirmado - " + brand.Active().Name

	type itemData struct {
		ProductName     string
		ProductMediaURL string
		Quantity        int
		TotalPrice      string
	}

	items := []itemData{}
	for _, item := range order.Items {
		prod := uc.findProductByID(item.ProductID)
		if prod != nil {
			var mediaURL string
			for _, variant := range prod.Variants {
				if variant.ID == item.VariantID && len(variant.Medias) > 0 {
					mediaURL = variant.Medias[0].URL
					break
				}
			}

			if mediaURL == "" {
				mediaURL = "https://via.placeholder.com/80x80/4A90E2/FFFFFF?text=IMG"
			}

			items = append(items, itemData{
				ProductName:     prod.Name,
				ProductMediaURL: mediaURL,
				Quantity:        item.Quantity,
				TotalPrice:      fmt.Sprintf("%.2f", item.TotalPrice),
			})
		}
	}

	if err := uc.emailService.SendTemplate(uc.getUserEmailFromRequest(customerName), subject, "order_confirmation.html", map[string]interface{}{
		"OrderID":      order.ID,
		"CustomerName": customerName,
		"TotalAmount":  fmt.Sprintf("%.2f", order.TotalAmount),
		"Items":        items,
	}); err != nil {
		return
	}
}

func (uc *checkoutUseCase) getUserEmailFromRequest(customerName string) string {
	return customerName
}

func (uc *checkoutUseCase) findProductByID(productID string) *product.Product {
	products, _ := uc.productRepo.FindByIDs([]string{productID})
	if len(products) > 0 {
		return products[0]
	}
	return nil
}

func (uc *checkoutUseCase) loadAndRenderTemplate(filePath string, data map[string]interface{}) (string, error) {
	return "", fmt.Errorf("loadAndRenderTemplate is deprecated; use EmailService.SendTemplate instead")
}

func (uc *checkoutUseCase) computeOwnerAllocations(ownerPercentages map[string]float64, supplierCosts map[string]float64, grossAmount float64) map[string]float64 {
	res := make(map[string]float64)
	if len(ownerPercentages) == 0 {
		return res
	}

	var totalSupplierCost float64
	for _, v := range supplierCosts {
		totalSupplierCost += v
	}
	liquidRevenue := grossAmount - totalSupplierCost
	if liquidRevenue <= 0 {
		for id := range ownerPercentages {
			res[id] = 0
		}
		return res
	}

	liquidCents := int(math.Round(liquidRevenue * 100))

	type ofrac struct {
		id    string
		floor int
		frac  float64
	}
	owners := make([]ofrac, 0, len(ownerPercentages))
	sumFloor := 0
	for id, pct := range ownerPercentages {
		exact := float64(liquidCents) * pct / 100.0
		f := int(math.Floor(exact))
		owners = append(owners, ofrac{id: id, floor: f, frac: exact - float64(f)})
		sumFloor += f
	}

	remainder := liquidCents - sumFloor
	sort.SliceStable(owners, func(i, j int) bool {
		return owners[i].frac > owners[j].frac
	})

	alloc := make(map[string]int, len(owners))
	for _, o := range owners {
		alloc[o.id] = o.floor
	}
	for i := 0; i < remainder && i < len(owners); i++ {
		alloc[owners[i].id]++
	}

	for id, cents := range alloc {
		res[id] = float64(cents) / 100.0
	}
	return res
}
