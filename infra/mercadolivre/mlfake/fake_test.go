package mlfake_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"vozko/domain/cache"
	mldomain "vozko/domain/mercadolivre"
	mlinfra "vozko/infra/mercadolivre"
	"vozko/infra/mercadolivre/mlfake"
)

const (
	appID    = "5503910054141466"
	secret   = "fake-secret"
	redirect = "https://api.example.com/oauth/mercadolivre/callback"
	sellerID = "415458330"
	buyerID  = "123456789"
)

type openLimiter struct{}

func (openLimiter) Allow(string) (bool, time.Duration, error) { return true, 0, nil }

func openFactory(string, int, time.Duration) cache.RateLimiter { return openLimiter{} }

type stack struct {
	fake      *mlfake.Server
	oauth     *mlinfra.OAuthService
	users     *mlinfra.UserService
	messages  *mlinfra.MessageService
	questions *mlinfra.QuestionService
	sales     *mlinfra.SalesService
}

func newStack(t *testing.T) stack {
	t.Helper()
	fake := mlfake.New(mlfake.Config{AppID: appID, ClientSecret: secret, RedirectURI: redirect}).Start()
	t.Cleanup(fake.Close)
	client, err := mlinfra.NewClient(mlinfra.Config{BaseURL: fake.URL(), RateLimiterFactory: openFactory, Backoff: func(int) time.Duration { return time.Millisecond }})
	if err != nil {
		t.Fatal(err)
	}
	oauth, err := mlinfra.NewOAuthService(client, mlinfra.OAuthConfig{AppID: appID, ClientSecret: secret, RedirectURI: redirect, AuthorizeURL: fake.AuthorizeURL()})
	if err != nil {
		t.Fatal(err)
	}
	users, _ := mlinfra.NewUserService(client)
	messages, _ := mlinfra.NewMessageService(client)
	questions, _ := mlinfra.NewQuestionService(client)
	sales, _ := mlinfra.NewSalesService(client)
	return stack{fake: fake, oauth: oauth, users: users, messages: messages, questions: questions, sales: sales}
}

func (s stack) connect(t *testing.T) *mldomain.TokenGrant {
	t.Helper()
	verifier, _ := mlinfra.NewPKCEVerifier()
	challenge, _ := mlinfra.PKCEChallenge(verifier)
	target, err := s.oauth.AuthorizeURL("state-1", challenge)
	if err != nil {
		t.Fatal(err)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || back.Query().Get("state") != "state-1" || back.Query().Get("code") == "" {
		t.Fatalf("authorize redirect = %q", resp.Header.Get("Location"))
	}
	grant, err := s.oauth.ExchangeCode(context.Background(), back.Query().Get("code"), verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	return grant
}

func TestTheFakeSpeaksTheProtocolTheClientExpects(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.fake.AddSeller(mlfake.SellerSpec{ID: sellerID, Nickname: "LOJA_TESTE"})
	grant := s.connect(t)
	if grant.UserID != sellerID || !mldomain.HasRequiredScopes(grant.Scopes) || grant.ExpiresIn != 6*time.Hour {
		t.Fatalf("grant = %+v", grant)
	}
	creds := mldomain.Credentials{UserID: grant.UserID, AccessToken: grant.AccessToken}
	me, err := s.users.Me(ctx, creds)
	if err != nil || me.ID != sellerID || me.SiteID != mldomain.SiteMLB || me.Nickname != "LOJA_TESTE" {
		t.Fatalf("me = %+v %v", me, err)
	}

	s.fake.AddPack(mlfake.PackSpec{PackID: "2000000089077943", SellerID: sellerID, BuyerID: buyerID, Items: []mlfake.PackItem{{ItemID: "MLB100", Title: "Caneca", Quantity: 2, UnitPrice: 10}}})
	id := s.fake.BuyerWrites("2000000089077943", "Oi, chegou?")
	msg, err := s.messages.GetMessage(ctx, creds, id)
	if err != nil || msg.PackKey != "2000000089077943" || msg.FromUserID != buyerID || msg.Text != "Oi, chegou?" {
		t.Fatalf("message = %+v %v", msg, err)
	}
	thread, err := s.messages.Thread(ctx, creds, mldomain.ThreadQuery{PackKey: "2000000089077943", Limit: 10})
	if err != nil || thread.Status != "active" || thread.Total != 1 || mldomain.MediationFromPath(thread.Path) == nil || *mldomain.MediationFromPath(thread.Path) {
		t.Fatalf("thread = %+v %v", thread, err)
	}
	pack, err := s.sales.GetPack(ctx, creds, "2000000089077943")
	if err != nil || pack.BuyerID != buyerID || len(pack.OrderIDs) != 1 {
		t.Fatalf("pack = %+v %v", pack, err)
	}
	order, err := s.sales.GetOrder(ctx, creds, pack.OrderIDs[0])
	if err != nil || len(order.Items) != 1 || order.TotalAmount != 20 || order.BuyerID != buyerID {
		t.Fatalf("order = %+v %v", order, err)
	}
	sent, err := s.messages.Send(ctx, creds, mldomain.SendMessage{PackKey: "2000000089077943", ToUserID: buyerID, Text: "Chegou sim"})
	if err != nil || sent.ID == "" || sent.PendingReview {
		t.Fatalf("sent = %+v %v", sent, err)
	}

	qid := s.fake.Ask(sellerID, "MLB100", buyerID, "Tem azul?")
	q, err := s.questions.GetQuestion(ctx, creds, qid)
	if err != nil || q.Status != mldomain.QuestionUnanswered || q.FromUserID != buyerID || q.SellerID != sellerID {
		t.Fatalf("question = %+v %v", q, err)
	}
	if _, err := s.questions.Answer(ctx, creds, qid, "Tem sim"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.questions.Answer(ctx, creds, qid, "De novo"); err == nil {
		t.Fatal("a second answer must be refused")
	}
	if s.fake.AnswersPosted(qid) != 1 {
		t.Fatalf("answers = %d", s.fake.AnswersPosted(qid))
	}
}

func TestRefreshTokensRotateAndAReusedOneIsADeadGrant(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.fake.AddSeller(mlfake.SellerSpec{ID: sellerID})
	grant := s.connect(t)
	next, err := s.oauth.Refresh(ctx, mldomain.RefreshCredentials{UserID: sellerID, RefreshToken: grant.RefreshToken})
	if err != nil || next.RefreshToken == grant.RefreshToken {
		t.Fatalf("refresh = %+v %v", next, err)
	}
	_, err = s.oauth.Refresh(ctx, mldomain.RefreshCredentials{UserID: sellerID, RefreshToken: grant.RefreshToken})
	if !mlinfra.IsAuth(err) {
		t.Fatalf("reusing a rotated refresh token must be invalid_grant: %v", err)
	}
}

func TestPermissionAndOperatorRefusalsLookLikeMercadoLivre(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.fake.AddSeller(mlfake.SellerSpec{ID: sellerID, DeniedPermission: true})
	grant := s.connect(t)
	creds := mldomain.Credentials{UserID: grant.UserID, AccessToken: grant.AccessToken}
	if _, err := s.messages.Unread(ctx, creds); !mlinfra.IsPermission(err) {
		t.Fatalf("unread = %v", err)
	}
	if _, err := s.users.Me(ctx, mldomain.Credentials{UserID: sellerID, AccessToken: "APP_USR-unknown"}); !mlinfra.IsAuth(err) {
		t.Fatalf("an unknown token must be an auth error: %v", err)
	}
}
