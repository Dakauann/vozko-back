package mercadolivre_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/infra/database/schema"
	"vozko/infra/mercadolivre/mlfake"
	mluc "vozko/usecases/mercadolivre"
)

type pack struct {
	id    string
	buyer string
}

func (h *harness) newPack(seller string, mediated bool) pack {
	h.t.Helper()
	p := pack{id: "20000" + h.fake.NextID(), buyer: "9" + h.fake.NextID()[3:]}
	h.fake.AddPack(mlfake.PackSpec{
		PackID: p.id, SellerID: seller, BuyerID: p.buyer, Mediated: mediated,
		Items: []mlfake.PackItem{{ItemID: "MLB" + h.fake.NextID(), Title: "Caneca térmica 500 ml", Quantity: 2, UnitPrice: 59.9}},
	})
	return p
}

func (h *harness) conversationOf(accountID, packID string) *mldomain.Conversation {
	h.t.Helper()
	conv, err := h.conversations.FindByPack(h.ctx, accountID, packID)
	if err != nil {
		h.t.Fatalf("no conversation for pack %s: %v", packID, err)
	}
	return conv
}

func (h *harness) operatorSend(conversationID, text string) error {
	_, err := h.sender.SendTextMessage(conversationID, "mercadolivre", text, conversation.SendOrigin{SenderUserID: h.userID})
	return err
}

func (h *harness) window(conversationID string) (conversation.WindowState, conversation.WindowState) {
	h.t.Helper()
	ec, err := h.adapter.ResolveEntry(h.ctx, conversationID)
	if err != nil {
		h.t.Fatal(err)
	}
	single, err := h.adapter.WindowState(h.ctx, ec)
	if err != nil {
		h.t.Fatal(err)
	}
	list, err := h.adapter.WindowStates(h.ctx, []string{conversationID})
	if err != nil {
		h.t.Fatal(err)
	}
	return single, list[conversationID]
}

func TestMercadoLivrePipelineEndToEnd(t *testing.T) {
	h := newHarness(t)

	t.Run("01 connect persists the account and refuses what it must", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(1), Nickname: "LOJA_UM"})
		if account.Status != mldomain.StatusConnected || account.SiteID != mldomain.SiteMLB || account.Nickname != "LOJA_UM" ||
			!account.CanReadQuestions || !account.CanAnswerQuestions || !account.CanReadMessages || !account.CanSendMessages ||
			!mldomain.HasRequiredScopes(account.GrantedScopes) || account.AccessToken == "" || account.RefreshToken == "" {
			t.Fatalf("connected account = %+v", account)
		}
		var stored string
		if err := h.db.Raw("SELECT access_token::text FROM mercadolivre_accounts WHERE id = ?", account.ID).Scan(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored == "" || strings.Contains(stored, "APP_USR") {
			t.Fatal("the access token is stored in clear text")
		}
		detail := h.call(http.MethodGet, "/mercadolivre/accounts/"+account.ID, h.workspaceID, nil)
		var view struct {
			Status          string `json:"status"`
			CanSendMessages bool   `json:"canSendMessages"`
			MLUserID        string `json:"mlUserId"`
		}
		detail.decode(t, &view)
		if detail.status != http.StatusOK || view.Status != "CONNECTED" || !view.CanSendMessages || view.MLUserID != sellerID(1) {
			t.Fatalf("account detail %d %s", detail.status, detail.body)
		}

		refusals := []struct {
			name   string
			seller mlfake.SellerSpec
			space  string
			reason string
		}{
			{"operator user", mlfake.SellerSpec{ID: sellerID(90), Operator: true}, h.workspaceID, "not_account_admin"},
			{"site other than MLB", mlfake.SellerSpec{ID: sellerID(91), SiteID: "MLA"}, h.workspaceID, "site_not_supported"},
			{"missing functional permission", mlfake.SellerSpec{ID: sellerID(92), DeniedPermission: true}, h.workspaceID, "missing_permission"},
			{"already linked in another workspace", mlfake.SellerSpec{ID: sellerID(1), Nickname: "LOJA_UM"}, uuid.NewString(), "already_linked"},
		}
		for _, tc := range refusals {
			h.fake.AddSeller(tc.seller)
			h.fake.SetDefaultSeller(tc.seller.ID)
			got := h.connectSeller(tc.space)
			if got.status != "error" || got.reason != tc.reason {
				t.Errorf("%s: connect = %+v, want reason %s", tc.name, got, tc.reason)
			}
			if tc.seller.ID != sellerID(1) && h.rows("mercadolivre_accounts", "ml_user_id = ?", tc.seller.ID) != 0 {
				t.Errorf("%s: an account row was written", tc.name)
			}
		}
		if n := h.rows("mercadolivre_accounts", "ml_user_id = ?", sellerID(1)); n != 1 {
			t.Fatalf("seller 1 has %d account rows", n)
		}
	})

	t.Run("02 a buyer message reaches the CRM and the agent answers within 350 characters", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(2)})
		h.enableAgent(account.ID)
		p := h.newPack(account.MLUserID, false)
		h.ai.script(strings.Repeat("Seu pedido já está a caminho. ", 15), e2eAgentReply)
		msgID := h.buyerMessage(account.MLUserID, p.id, "Oi! Meu pedido já saiu?")

		conv := h.conversationOf(account.ID, p.id)
		contact, err := h.contacts.FindByID(h.ctx, conv.ContactID)
		if err != nil || contact.MLUserID != p.buyer || contact.Nickname != "COMPRADOR"+p.buyer {
			t.Fatalf("contact = %+v %v", contact, err)
		}
		if conv.PackSummary == nil || len(conv.PackSummary.Items) != 1 || conv.PackSummary.Items[0].Title != "Caneca térmica 500 ml" ||
			conv.PackSummary.OrderStatus != "paid" || conv.PackSummary.ShippingStatus != "shipped" || conv.PackSummary.TotalAmount != 119.8 {
			t.Fatalf("pack summary = %+v", conv.PackSummary)
		}
		if conv.PackSummary.Items[0].ThumbnailKey == "" || h.files.withPrefix(conv.PackSummary.Items[0].ThumbnailKey) != 1 {
			t.Fatalf("the item thumbnail was not re-hosted: %+v", conv.PackSummary.Items[0])
		}
		stored, err := h.messages.GetByEntryAndExternalMessageID("mercadolivre", conv.ID, msgID)
		if err != nil || stored.Text != "Oi! Meu pedido já saiu?" || !stored.SentBy.IsContact() {
			t.Fatalf("transcript row = %+v %v", stored, err)
		}
		if h.assignments.count(conv.ID, account.ID) != 1 {
			t.Fatalf("assignment calls = %d", h.assignments.count(conv.ID, account.ID))
		}
		sends := h.fake.Sends(p.id)
		if len(sends) != 1 || sends[0].To != p.buyer || sends[0].From != account.MLUserID || sends[0].Text != e2eAgentReply || runes(sends[0].Text) > 350 {
			t.Fatalf("Mercado Livre received %+v", sends)
		}
		if h.aiReplies(conv.ID) != 1 {
			t.Fatalf("ai replies in the transcript = %d", h.aiReplies(conv.ID))
		}
		inputs := h.ai.generated()
		if len(inputs) != 2 || !strings.Contains(inputs[0].SystemPrompt, "350") {
			t.Fatalf("the agent was not told the 350 character rule or not asked to rewrite: %d generations", len(inputs))
		}
		if h.inbound(conv.ID) != 1 {
			t.Fatalf("inbound rows = %d", h.inbound(conv.ID))
		}
	})

	t.Run("03 an agent mediated thread allows one reply per inbound message", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(3)})
		h.enableAgent(account.ID)
		p := h.newPack(account.MLUserID, true)
		h.buyerMessage(account.MLUserID, p.id, "Quero trocar o tamanho")
		conv := h.conversationOf(account.ID, p.id)
		if conv.AgentMediated == nil || !*conv.AgentMediated || conv.OutboundBudget != 1 || conv.BuyerMLUserID != p.buyer {
			t.Fatalf("conversation = %+v", conv)
		}
		sends := h.fake.Sends(p.id)
		if len(sends) != 1 || sends[0].To != mlfake.AgentUserID {
			t.Fatalf("the agent reply must go to the messaging agent: %+v", sends)
		}
		if err := h.operatorSend(conv.ID, "Pode deixar"); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
			t.Fatalf("a second send before the next inbound must be refused: %v", err)
		}
		single, _ := h.window(conv.ID)
		if single.Open || single.Reason != conversation.WindowReasonBudgetExhausted {
			t.Fatalf("window = %+v", single)
		}
		if len(h.fake.Sends(p.id)) != 1 {
			t.Fatal("the refused send reached Mercado Livre")
		}
		h.buyerMessage(account.MLUserID, p.id, "Alguém aí?")
		if sends := h.fake.Sends(p.id); len(sends) != 2 || sends[1].To != mlfake.AgentUserID {
			t.Fatalf("a new inbound must reopen exactly one reply: %+v", sends)
		}
		if err := h.operatorSend(conv.ID, "Mais uma"); !errors.Is(err, conversation.ErrOutboundWindowClosed) {
			t.Fatalf("budget not enforced after the second inbound: %v", err)
		}
	})

	t.Run("04 a duplicated notification makes one transcript row and one agent reply", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(4)})
		h.enableAgent(account.ID)
		p := h.newPack(account.MLUserID, false)
		msgID := h.fake.BuyerWrites(p.id, "Vocês emitem nota fiscal?")
		same := h.fake.Notification("messages", account.MLUserID, msgID, uuid.NewString())
		if h.notify(same) != http.StatusOK || h.notify(same) != http.StatusOK {
			t.Fatal("notifications refused")
		}
		h.settle()
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.notify(h.fake.Notification("messages", account.MLUserID, msgID, uuid.NewString()))
			}()
		}
		wg.Wait()
		h.settle()
		conv := h.conversationOf(account.ID, p.id)
		if h.inbound(conv.ID) != 1 {
			t.Fatalf("inbound rows = %d", h.inbound(conv.ID))
		}
		if sends := h.fake.Sends(p.id); len(sends) != 1 {
			t.Fatalf("the agent answered %d times", len(sends))
		}

		cold := h.fake.BuyerWrites(p.id, "E qual o prazo de troca?")
		start := make(chan struct{})
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h.notify(h.fake.Notification("messages", account.MLUserID, cold, uuid.NewString()))
			}()
		}
		close(start)
		wg.Wait()
		h.settle()
		if h.inbound(conv.ID) != 2 {
			t.Fatalf("inbound rows after a cold concurrent burst = %d", h.inbound(conv.ID))
		}
		if sends := h.fake.Sends(p.id); len(sends) != 2 {
			t.Fatalf("a burst of notifications for one message made %d agent replies in total", len(sends))
		}
	})

	t.Run("05 send outcomes: pending review, 429, dropped connection and a mediation block", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(5)})
		p := h.newPack(account.MLUserID, false)
		h.buyerMessage(account.MLUserID, p.id, "Chegou quebrado")
		conv := h.conversationOf(account.ID, p.id)

		h.fake.PlanSends(mlfake.SendPendingReview)
		if err := h.operatorSend(conv.ID, "Vamos resolver, me manda uma foto?"); err != nil {
			t.Fatal(err)
		}
		pending := h.fake.Sends(p.id)
		var stored schema.ConversationMessage
		if err := h.db.Where("entry_id = ? AND sender_kind = ?", conv.ID, string(conversation.SenderHuman)).Order("created_at desc").First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(stored.Metadata), "pending_review") || stored.ExternalMessageID == nil {
			t.Fatalf("pending review not stored on the message: %s", stored.Metadata)
		}

		h.fake.PlanSends(mlfake.SendRateLimited)
		if err := h.operatorSend(conv.ID, "Recebemos a foto"); err != nil {
			t.Fatalf("a 429 refused before acting must be retried: %v", err)
		}
		if got := len(h.fake.Sends(p.id)) - len(pending); got != 2 {
			t.Fatalf("429 then success should be 2 POSTs, got %d", got)
		}

		before := len(h.fake.Sends(p.id))
		h.fake.PlanSends(mlfake.SendConnectionDropped)
		if err := h.operatorSend(conv.ID, "Enviamos outro"); err == nil {
			t.Fatal("a dropped connection must surface as a failure")
		}
		if got := len(h.fake.Sends(p.id)) - before; got != 1 {
			t.Fatalf("a non idempotent POST was retried after a transport failure: %d attempts", got)
		}

		h.fake.PlanSends(mlfake.SendBlockedByMediation)
		err := h.operatorSend(conv.ID, "Última mensagem")
		if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
			t.Fatalf("a blocked send must close the window: %v", err)
		}
		single, list := h.window(conv.ID)
		if single.Open || single.Reason != conversation.WindowReasonProviderBlocked || single.ProviderDetail != "blocked_by_mediation" {
			t.Fatalf("single entry window = %+v", single)
		}
		if list != single {
			t.Fatalf("list window %+v differs from the single entry %+v", list, single)
		}
		fresh := h.conversationOf(account.ID, p.id)
		if fresh.MLStatus != "blocked" || fresh.MLSubstatus != "blocked_by_mediation" || fresh.WindowOpen {
			t.Fatalf("mirrored status = %s/%s open=%v", fresh.MLStatus, fresh.MLSubstatus, fresh.WindowOpen)
		}
		before = len(h.fake.Sends(p.id))
		if err := h.operatorSend(conv.ID, "Ainda bloqueado?"); !errors.Is(err, conversation.ErrOutboundWindowClosed) || len(h.fake.Sends(p.id)) != before {
			t.Fatalf("a closed window must refuse locally: %v", err)
		}
	})

	t.Run("06 questions: rules once under concurrency, manual answer, delete and block", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(6)})
		rule := h.call(http.MethodPost, "/mercadolivre/accounts/"+account.ID+"/comment-rules", h.workspaceID, map[string]any{
			"name": "Frete", "enabled": true, "match": "contains", "keywords": []string{"frete"},
			"actions": []string{"public_reply"}, "publicReplyText": "Oi {{username}}, o frete é grátis para todo o Brasil!",
		})
		if rule.status != http.StatusCreated && rule.status != http.StatusOK {
			t.Fatalf("rule create answered %d: %s", rule.status, rule.body)
		}
		bad := h.call(http.MethodPost, "/mercadolivre/accounts/"+account.ID+"/comment-rules", h.workspaceID, map[string]any{
			"name": "Privada", "enabled": true, "match": "any", "actions": []string{"private_reply"}, "privateReplyText": "oi",
		})
		if bad.status != http.StatusBadRequest {
			t.Fatalf("a private reply rule must be refused on Mercado Livre: %d %s", bad.status, bad.body)
		}

		asker := "77" + h.fake.NextID()
		qid := h.fake.Ask(account.MLUserID, "MLB555000111", asker, "Qual o valor do frete para SP?")
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.notify(h.fake.Notification("questions", account.MLUserID, "/questions/"+qid, uuid.NewString()))
			}()
		}
		wg.Wait()
		h.settle()
		if n := h.fake.AnswersPosted(qid); n != 1 {
			t.Fatalf("the rule answered %d times", n)
		}
		mirror, err := h.questionsRepo.FindByQuestionID(h.ctx, account.ID, qid)
		if err != nil || mirror.Status != mldomain.QuestionAnswered || mirror.AnsweredBy != mldomain.AnsweredByRule || mirror.RuleID == nil {
			t.Fatalf("mirror = %+v %v", mirror, err)
		}

		answered := h.fake.Ask(account.MLUserID, "MLB555000111", asker, "E o frete pro RJ?")
		h.fake.AnswerInApp(answered, "Também grátis")
		h.notify(h.fake.Notification("questions", account.MLUserID, "/questions/"+answered, uuid.NewString()))
		h.settle()
		if h.fake.AnswersPosted(answered) != 0 {
			t.Fatal("a question already answered ran a rule")
		}
		mirror, err = h.questionsRepo.FindByQuestionID(h.ctx, account.ID, answered)
		if err != nil || mirror.AnsweredBy != mldomain.AnsweredByMercadoLivreApp {
			t.Fatalf("answered elsewhere = %+v %v", mirror, err)
		}

		manual := h.fake.Ask(account.MLUserID, "MLB555000111", asker, "Tem na cor azul?")
		h.notify(h.fake.Notification("questions", account.MLUserID, "/questions/"+manual, uuid.NewString()))
		h.settle()
		path := "/mercadolivre/accounts/" + account.ID + "/questions/" + manual
		first := h.call(http.MethodPost, path+"/answer", h.workspaceID, map[string]string{"text": "Temos sim!"})
		if first.status != http.StatusOK {
			t.Fatalf("manual answer answered %d: %s", first.status, first.body)
		}
		second := h.call(http.MethodPost, path+"/answer", h.workspaceID, map[string]string{"text": "Temos sim, de novo"})
		if second.status != http.StatusConflict || h.fake.AnswersPosted(manual) != 1 {
			t.Fatalf("second answer answered %d (posted %d): %s", second.status, h.fake.AnswersPosted(manual), second.body)
		}

		doomed := h.fake.Ask(account.MLUserID, "MLB555000111", "88"+h.fake.NextID(), "Compra comigo por fora?")
		h.notify(h.fake.Notification("questions", account.MLUserID, "/questions/"+doomed, uuid.NewString()))
		h.settle()
		if res := h.call(http.MethodDelete, "/mercadolivre/accounts/"+account.ID+"/questions/"+doomed, h.workspaceID, nil); res.status != http.StatusOK {
			t.Fatalf("delete answered %d: %s", res.status, res.body)
		}
		if h.fake.QuestionStatus(doomed) != "DELETED" {
			t.Fatalf("Mercado Livre still has the question as %s", h.fake.QuestionStatus(doomed))
		}
		if mirror, err := h.questionsRepo.FindByQuestionID(h.ctx, account.ID, doomed); err != nil || mirror.Status != mldomain.QuestionDeleted {
			t.Fatalf("deleted mirror = %+v %v", mirror, err)
		}

		if res := h.call(http.MethodPost, path+"/block-author", h.workspaceID, nil); res.status != http.StatusOK {
			t.Fatalf("block answered %d: %s", res.status, res.body)
		}
		if !h.fake.Blacklisted(account.MLUserID, asker) {
			t.Fatal("the asker is not on the Mercado Livre blacklist")
		}
		got := h.call(http.MethodGet, path, h.workspaceID, nil)
		var view struct {
			Asker struct {
				Blocked bool `json:"blocked"`
			} `json:"asker"`
			AnsweredBy string `json:"answeredBy"`
		}
		got.decode(t, &view)
		if got.status != http.StatusOK || !view.Asker.Blocked || view.AnsweredBy != "operator" {
			t.Fatalf("question view %d %s", got.status, got.body)
		}
	})

	t.Run("07 reconciliation finds what the webhook never delivered and missed feeds replay", func(t *testing.T) {
		h.t = t
		account := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(7)})
		p := h.newPack(account.MLUserID, false)
		silent := h.fake.BuyerWrites(p.id, "Mensagem que o webhook perdeu")
		qid := h.fake.Ask(account.MLUserID, "MLB777000111", "66"+h.fake.NextID(), "Pergunta que o webhook perdeu")
		before := h.metrics.count("reconcile/published")
		if err := h.reconciler.Execute(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.settle()
		conv := h.conversationOf(account.ID, p.id)
		if _, err := h.messages.GetByEntryAndExternalMessageID("mercadolivre", conv.ID, silent); err != nil {
			t.Fatalf("the unread thread was not reconciled: %v", err)
		}
		if _, err := h.questionsRepo.FindByQuestionID(h.ctx, account.ID, qid); err != nil {
			t.Fatalf("the unanswered question was not reconciled: %v", err)
		}
		if h.metrics.count("reconcile/published") < before+2 {
			t.Fatalf("reconciliation published %d jobs", h.metrics.count("reconcile/published")-before)
		}

		missed := h.fake.BuyerWrites(p.id, "Mensagem das notificações perdidas")
		if err := h.fake.MissNotification(h.fake.Notification("messages", account.MLUserID, missed, uuid.NewString())); err != nil {
			t.Fatal(err)
		}
		if err := h.missed.Execute(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.settle()
		if _, err := h.messages.GetByEntryAndExternalMessageID("mercadolivre", conv.ID, missed); err != nil {
			t.Fatalf("the missed feed was not replayed: %v", err)
		}
	})

	t.Run("08 token refresh is single flight and a dead grant asks for a reconnect", func(t *testing.T) {
		h.t = t
		seller := sellerID(8)
		h.fake.AddSeller(mlfake.SellerSpec{ID: seller})
		h.fake.SetAccessTTL(seller, 5*time.Minute)
		h.fake.SetDefaultSeller(seller)
		connected := h.connectSeller(h.workspaceID)
		if connected.status != "connected" {
			t.Fatalf("connect = %+v", connected)
		}
		account, err := h.accounts.FindByID(h.ctx, connected.accountID)
		if err != nil {
			t.Fatal(err)
		}
		h.fake.SetAccessTTL(seller, 6*time.Hour)
		if !account.TokenNeedsRefresh(time.Now().UTC()) {
			t.Fatal("the account should need a refresh")
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		var ok, busy, other int
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fresh, _ := h.accounts.FindByID(h.ctx, account.ID)
				_, err := h.tokens.Credentials(h.ctx, fresh)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, mluc.ErrTokenRefreshInProgress):
					busy++
				default:
					other++
					t.Errorf("credentials: %v", err)
				}
			}()
		}
		wg.Wait()
		if n := h.fake.RefreshCount(seller); n != 1 {
			t.Fatalf("Mercado Livre saw %d refreshes (ok=%d busy=%d)", n, ok, busy)
		}
		refreshed, _ := h.accounts.FindByID(h.ctx, account.ID)
		if refreshed.Status != mldomain.StatusConnected || refreshed.RefreshToken == account.RefreshToken || refreshed.TokenNeedsRefresh(time.Now().UTC()) {
			t.Fatalf("after refresh = %+v", refreshed)
		}
		if _, err := h.tokens.Credentials(h.ctx, refreshed); err != nil {
			t.Fatalf("fresh credentials refused: %v", err)
		}

		h.fake.RevokeGrant(seller)
		if _, err := h.tokens.RefreshExpiringBefore(h.ctx, account.ID, time.Now().Add(7*time.Hour)); err != nil {
			t.Fatal(err)
		}
		lost, _ := h.accounts.FindByID(h.ctx, account.ID)
		if lost.Status != mldomain.StatusNeedsReconnect {
			t.Fatalf("status after invalid_grant = %s", lost.Status)
		}
		found := false
		for _, d := range h.alerts.drafts() {
			if d.EpisodeKey == "mercadolivre_reconnect:"+account.ID && d.Email && d.WorkspaceID == h.workspaceID {
				found = true
			}
		}
		if !found {
			t.Fatalf("no reconnect episode published: %+v", h.alerts.drafts())
		}
		p := h.newPack(seller, false)
		h.buyerMessage(seller, p.id, "Ninguém responde?")
		if _, err := h.conversations.FindByPack(h.ctx, account.ID, p.id); !errors.Is(err, mldomain.ErrConversationNotFound) {
			t.Fatalf("an account that needs a reconnect must not ingest: %v", err)
		}
	})

	t.Run("09 disconnect then purge after 30 days deletes only that account", func(t *testing.T) {
		h.t = t
		doomed := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(9)})
		kept := h.connectNewSeller(mlfake.SellerSpec{ID: sellerID(10)})
		seed := func(a *mldomain.Account) *mldomain.Conversation {
			h.fake.AddItems(a.MLUserID, mlfake.ItemSpec{ID: "MLB9" + h.fake.NextID(), Title: "Item de Teste - Por favor, NÃO OFERTAR!"})
			rule := h.call(http.MethodPost, "/mercadolivre/accounts/"+a.ID+"/comment-rules", h.workspaceID, map[string]any{
				"name": "Apagar", "enabled": true, "match": "contains", "keywords": []string{"whatsapp"}, "actions": []string{"delete"},
			})
			if rule.status >= 300 {
				t.Fatalf("rule answered %d: %s", rule.status, rule.body)
			}
			p := h.newPack(a.MLUserID, false)
			h.buyerMessage(a.MLUserID, p.id, "Oi")
			q := h.fake.Ask(a.MLUserID, "MLB1", "55"+h.fake.NextID(), "Oi?")
			h.notify(h.fake.Notification("questions", a.MLUserID, "/questions/"+q, uuid.NewString()))
			h.settle()
			return h.conversationOf(a.ID, p.id)
		}
		doomedConv, keptConv := seed(doomed), seed(kept)
		if err := h.reconciler.Execute(h.ctx); err != nil {
			t.Fatal(err)
		}
		h.settle()
		if h.rows("mercadolivre_listings", "account_id = ?", doomed.ID) == 0 {
			t.Fatal("the listing sync wrote nothing to purge")
		}

		res := h.call(http.MethodDelete, "/mercadolivre/accounts/"+doomed.ID, h.workspaceID, nil)
		if res.status != http.StatusOK {
			t.Fatalf("disconnect answered %d: %s", res.status, res.body)
		}
		gone, err := h.accounts.FindByWorkspaceAndMLUserIDUnscoped(h.ctx, h.workspaceID, doomed.MLUserID)
		if err != nil || gone.PurgeAfter == nil || gone.AccessToken != "" {
			t.Fatalf("disconnected account = %+v %v", gone, err)
		}
		purgeAt := *gone.PurgeAfter
		if d := time.Until(purgeAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
			t.Fatalf("purge scheduled in %s", d)
		}

		mluc.SetPurgeClock(h.purge, func() time.Time { return purgeAt.Add(-24 * time.Hour) })
		if err := h.purge.Execute(h.ctx); err != nil {
			t.Fatal(err)
		}
		if h.rows("mercadolivre_conversations", "account_id = ?", doomed.ID) == 0 {
			t.Fatal("purged before the grace period ended")
		}
		mluc.SetPurgeClock(h.purge, func() time.Time { return purgeAt.Add(time.Hour) })
		if err := h.purge.Execute(h.ctx); err != nil {
			t.Fatal(err)
		}
		for table, where := range map[string]string{
			"mercadolivre_accounts":      "id = ?",
			"mercadolivre_contacts":      "account_id = ?",
			"mercadolivre_conversations": "account_id = ?",
			"mercadolivre_questions":     "account_id = ?",
			"mercadolivre_listings":      "account_id = ?",
			"mercadolivre_listing_syncs": "account_id = ?",
			"comment_rules":              "account_id = ?",
		} {
			var n int64
			if err := h.db.Unscoped().Table(table).Where(where, doomed.ID).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%s still holds %d rows of the purged account", table, n)
			}
		}
		if n := h.rows("conversation_messages", "entry_id = ?", doomedConv.ID); n != 0 {
			t.Errorf("%d transcript rows survived", n)
		}
		if n := h.files.withPrefix("mercadolivre/accounts/" + doomed.ID); n != 0 {
			t.Errorf("%d stored files survived", n)
		}
		for table, where := range map[string]string{
			"mercadolivre_accounts": "id = ?", "mercadolivre_contacts": "account_id = ?", "mercadolivre_conversations": "account_id = ?",
			"mercadolivre_questions": "account_id = ?", "mercadolivre_listings": "account_id = ?", "comment_rules": "account_id = ?",
		} {
			if h.rows(table, where, kept.ID) == 0 {
				t.Errorf("%s lost the other account's rows", table)
			}
		}
		if h.inbound(keptConv.ID) == 0 {
			t.Error("the other account lost its transcript")
		}
	})

	t.Run("10 forged notifications change nothing", func(t *testing.T) {
		h.t = t
		before := h.rows("mercadolivre_conversations", "1 = 1")
		if code := h.notifyWithToken("wrong-token-wrong-token-wrong-token-wrong-to", h.fake.Notification("messages", sellerID(2), "0123456789abcdef0123456789abcdef", "x")); code != http.StatusNotFound {
			t.Fatalf("wrong path token answered %d", code)
		}
		var forged map[string]any
		_ = json.Unmarshal(h.fake.Notification("questions", sellerID(2), "/questions/1", "y"), &forged)
		forged["application_id"] = 1234567
		raw, _ := json.Marshal(forged)
		if code := h.notify(raw); code != http.StatusBadRequest {
			t.Fatalf("wrong application id answered %d", code)
		}
		if code := h.notify([]byte(`{"topic":"messages","resource":"../etc/passwd","user_id":1,"application_id":` + e2eAppID + `}`)); code != http.StatusBadRequest {
			t.Fatalf("a bad resource answered %d", code)
		}
		dropped := h.metrics.count("consume/unknown_account")
		if code := h.notify(h.fake.Notification("messages", "123123123", "0123456789abcdef0123456789abcdef", uuid.NewString())); code != http.StatusOK {
			t.Fatalf("an unknown seller is acknowledged and dropped later, got %d", code)
		}
		h.settle()
		if h.metrics.count("consume/unknown_account") != dropped+1 {
			t.Fatal("the unknown seller was not dropped with a counter")
		}
		if after := h.rows("mercadolivre_conversations", "1 = 1"); after != before {
			t.Fatalf("forged notifications wrote %d conversations", after-before)
		}
	})

	t.Logf("fake Mercado Livre calls: %v", h.fake.Calls())
	t.Logf("pipeline metrics: %v", h.metrics.events)
}
