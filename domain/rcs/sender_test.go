package rcs

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func validSender() Sender {
	return Sender{
		WorkspaceID:     "ws-1",
		BrandName:       "Loja do João",
		Description:     "A plataforma envia atualizações de pedidos e promoções em nome de Loja do João.",
		LogoURL:         "https://media.example.com/logo.png",
		BannerURL:       "https://media.example.com/banner.png",
		Color:           "#1A73E8",
		UseCase:         UseCasePromotional,
		Sector:          SectorRetail,
		ContactName:     "João Silva",
		ContactEmail:    "marketing@lojadojoao.com.br",
		ContactPhone:    "+5511999990000",
		WebsiteURL:      "https://lojadojoao.com.br",
		PrivacyURL:      "https://lojadojoao.com.br/privacidade",
		TermsURL:        "https://lojadojoao.com.br/termos",
		CNPJ:            "11222333000181",
		BillingCategory: BillingNonConversational,
		Status:          SenderDraft,
	}
}

func TestSenderValidateAcceptsACompleteBrand(t *testing.T) {
	if err := validSender().Validate("vozkoia.com"); err != nil {
		t.Fatalf("Validate = %v", err)
	}
}

func TestSenderValidateRefusesWhatCarriersRefuse(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Sender)
		want   error
	}{
		{"missing brand name", func(s *Sender) { s.BrandName = " " }, ErrSenderIncomplete},
		{"brand name too long", func(s *Sender) { s.BrandName = string(make([]rune, MaxBrandNameRunes+1)) }, ErrSenderIncomplete},
		{"description too long", func(s *Sender) { s.Description = string(make([]rune, MaxBrandDescriptionRunes+1)) }, ErrSenderIncomplete},
		{"description that does not name the brand", func(s *Sender) { s.Description = "Envia promoções." }, ErrDescriptionNotISV},
		{"logo over http", func(s *Sender) { s.LogoURL = "http://media.example.com/logo.png" }, ErrSenderIncomplete},
		{"color not hex", func(s *Sender) { s.Color = "blue" }, ErrSenderIncomplete},
		{"unknown use case", func(s *Sender) { s.UseCase = "spam" }, ErrSenderIncomplete},
		{"unknown billing category", func(s *Sender) { s.BillingCategory = "free" }, ErrSenderIncomplete},
		{"free mailbox contact", func(s *Sender) { s.ContactEmail = "joao@gmail.com" }, ErrContactEmailDomain},
		{"platform contact", func(s *Sender) { s.ContactEmail = "suporte@vozkoia.com" }, ErrContactEmailDomain},
		{"platform subdomain contact", func(s *Sender) { s.ContactEmail = "a@mail.vozkoia.com" }, ErrContactEmailDomain},
		{"malformed contact email", func(s *Sender) { s.ContactEmail = "joao" }, ErrContactEmailDomain},
		{"cnpj with the wrong length", func(s *Sender) { s.CNPJ = "1122233300018" }, ErrSenderIncomplete},
		{"political sender", func(s *Sender) { s.Sector = SectorPolitical }, ErrSectorRefused},
		{"adult sender", func(s *Sender) { s.Sector = SectorAdult }, ErrSectorRefused},
		{"betting without authorization", func(s *Sender) { s.Sector = SectorGambling }, ErrGamblingAuthorization},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSender()
			tc.mutate(&s)
			if err := s.Validate("vozkoia.com"); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBettingIsAcceptedWithItsAuthorization(t *testing.T) {
	s := validSender()
	s.Sector = SectorGambling
	s.GamblingAuthorizationURL = "https://media.example.com/portaria.pdf"
	if err := s.Validate("vozkoia.com"); err != nil {
		t.Fatalf("Validate = %v", err)
	}
}

func TestCNPJIsStoredAsDigits(t *testing.T) {
	s := validSender()
	s.CNPJ = "11.222.333/0001-81"
	s.Normalize()
	if s.CNPJ != "11222333000181" {
		t.Fatalf("CNPJ = %q", s.CNPJ)
	}
}

func TestISVDescriptionNamesThePlatformAndTheBrand(t *testing.T) {
	got := ISVDescription("Vozko", "Loja do João", UseCasePromotional)
	want := "Vozko envia promoções e novidades em nome de Loja do João."
	if got != want {
		t.Fatalf("ISVDescription = %q, want %q", got, want)
	}
}

func TestSenderLifecycle(t *testing.T) {
	s := validSender()
	if s.CanSend() || !s.Editable() {
		t.Fatal("a draft cannot send and stays editable")
	}

	s, err := s.Submit(t0)
	if err != nil || s.Status != SenderSubmitted || s.SubmittedAt == nil || s.Editable() {
		t.Fatalf("Submit = %+v, %v", s, err)
	}

	s = s.ApplyProviderState(ProviderAwaitingBrand, "", t0)
	if s.Status != SenderAwaitingConfirmation {
		t.Fatalf("status = %s, want awaiting confirmation", s.Status)
	}

	s = s.ApplyProviderState(ProviderLaunched, "", t0.Add(time.Hour))
	if !s.CanSend() || s.LaunchedAt == nil {
		t.Fatalf("launched sender should send: %+v", s)
	}

	s, err = s.Pause(PausedByPlatform, PauseOptOutRate, t0.Add(2*time.Hour))
	if err != nil || s.CanSend() || s.Status != SenderPaused {
		t.Fatalf("Pause = %+v, %v", s, err)
	}

	s, err = s.Resume(t0.Add(3 * time.Hour))
	if err != nil || !s.CanSend() || s.PausedBy != "" || s.PauseReason != "" {
		t.Fatalf("Resume = %+v, %v", s, err)
	}
}

func TestSubmitRefusesAnIncompleteOrAlreadySubmittedSender(t *testing.T) {
	s := validSender()
	s.BrandName = ""
	if _, err := s.Submit(t0); !errors.Is(err, ErrSenderIncomplete) {
		t.Fatalf("Submit incomplete = %v", err)
	}
	s = validSender()
	s.Status = SenderSubmitted
	if _, err := s.Submit(t0); !errors.Is(err, ErrSenderTransition) {
		t.Fatalf("Submit twice = %v", err)
	}
}

func TestSubmitValidatesAgainstThePlatformDomainItWasGiven(t *testing.T) {
	s := validSender()
	s.PlatformEmailDomain = "lojadojoao.com.br"
	if _, err := s.Submit(t0); !errors.Is(err, ErrContactEmailDomain) {
		t.Fatalf("Submit = %v, want contact domain refusal", err)
	}
}

func TestCarrierPauseCannotBeLiftedByTheCustomer(t *testing.T) {
	s := validSender()
	s.Status = SenderLaunched
	s = s.ApplyProviderState(ProviderSuspended, "carrier complaint", t0)
	if s.Status != SenderPaused || s.PausedBy != PausedByCarrier {
		t.Fatalf("suspension = %+v", s)
	}
	if _, err := s.Resume(t0); !errors.Is(err, ErrSenderTransition) {
		t.Fatalf("Resume carrier pause = %v", err)
	}
	s = s.ApplyProviderState(ProviderLaunched, "", t0.Add(time.Hour))
	if !s.CanSend() {
		t.Fatal("the carrier lifting its pause relaunches the sender")
	}
}

func TestPlatformPauseSurvivesAProviderLaunchedReport(t *testing.T) {
	s := validSender()
	s.Status = SenderLaunched
	s, _ = s.Pause(PausedByPlatform, PauseUnpaidBalance, t0)
	s = s.ApplyProviderState(ProviderLaunched, "", t0.Add(time.Minute))
	if s.CanSend() {
		t.Fatal("a provider status sync must not lift a platform pause")
	}
}

func TestRejectionReturnsTheSenderToEditing(t *testing.T) {
	s := validSender()
	s.Status = SenderSubmitted
	s = s.ApplyProviderState(ProviderRejected, "logo blurry", t0)
	if s.Status != SenderRejected || s.StatusReason != "logo blurry" || !s.Editable() {
		t.Fatalf("rejected = %+v", s)
	}
	if _, err := s.Submit(t0); err != nil {
		t.Fatalf("resubmit after fixing = %v", err)
	}
}

func TestBillingCategoryIsFixedOnceSubmitted(t *testing.T) {
	s := validSender()
	s.Status = SenderLaunched
	if _, err := s.ChangeBillingCategory(BillingConversational); !errors.Is(err, ErrBillingCategoryFixed) {
		t.Fatalf("ChangeBillingCategory = %v", err)
	}
	d := validSender()
	d, err := d.ChangeBillingCategory(BillingConversational)
	if err != nil || d.BillingCategory != BillingConversational {
		t.Fatalf("draft change = %+v, %v", d, err)
	}
}

func TestPauseNeedsALaunchedSender(t *testing.T) {
	if _, err := validSender().Pause(PausedByPlatform, PauseManual, t0); !errors.Is(err, ErrSenderTransition) {
		t.Fatalf("Pause draft = %v", err)
	}
}

func TestIncompleteSenderNamesTheField(t *testing.T) {
	s := validSender()
	s.LogoURL = ""
	var incomplete *IncompleteError
	if err := s.Validate("vozkoia.com"); !errors.As(err, &incomplete) || incomplete.Field != "logo_url" || !errors.Is(err, ErrSenderIncomplete) {
		t.Fatalf("Validate = %v", err)
	}
}

func TestOnlyAManualPauseIsResumableByTheWorkspace(t *testing.T) {
	s := validSender()
	s.Status = SenderLaunched
	manual, _ := s.Pause(PausedByPlatform, PauseManual, t0)
	optOuts, _ := s.Pause(PausedByPlatform, PauseOptOutRate, t0)
	carrier := s.ApplyProviderState(ProviderSuspended, "", t0)
	if !manual.ResumableByWorkspace() || optOuts.ResumableByWorkspace() || carrier.ResumableByWorkspace() || s.ResumableByWorkspace() {
		t.Fatal("only the workspace's own pause can be lifted by the workspace")
	}
}
