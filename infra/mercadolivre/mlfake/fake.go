package mlfake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	AgentUserID       = "3037675074"
	DefaultAccessTTL  = 6 * time.Hour
	grantedScope      = "offline_access read write"
	mediatedPathTail  = "/conversations/agent"
	thumbnailPathBase = "/_static/thumbnails/"
)

type Config struct {
	AppID                  string
	ClientSecret           string
	RedirectURI            string
	CallbackOverride       string
	PublicURL              string
	IDBase                 int64
	APIFixtureDir          string
	NotificationFixtureDir string
	WebhookURL             string
}

type SellerSpec struct {
	ID               string
	Nickname         string
	SiteID           string
	Operator         bool
	DeniedPermission bool
}

type PackItem struct {
	ItemID    string
	Title     string
	Quantity  int
	UnitPrice float64
}

type PackSpec struct {
	PackID     string
	SellerID   string
	BuyerID    string
	OrderIDs   []string
	ShipmentID string
	Items      []PackItem
	Mediated   bool
}

type ItemSpec struct {
	ID     string
	Title  string
	Status string
}

type Attachment struct {
	FileName    string
	ContentType string
	Data        []byte
}

type SendOutcome int

const (
	SendAccepted SendOutcome = iota
	SendPendingReview
	SendBlockedByMediation
	SendRateLimited
	SendConnectionDropped
)

type Sent struct {
	PackID      string
	From        string
	To          string
	Text        string
	Attachments []string
	Outcome     SendOutcome
}

type seller struct {
	spec         SellerSpec
	accessTTL    time.Duration
	refreshToken string
	revoked      bool
	refreshes    int
	blacklist    []string
	items        []ItemSpec
}

type accessToken struct {
	seller  string
	expires time.Time
}

type authCode struct {
	seller    string
	challenge string
	redirect  string
	used      bool
}

type message struct {
	id          string
	from        string
	to          string
	text        string
	status      string
	created     time.Time
	read        *time.Time
	attachments []string
}

type pack struct {
	spec      PackSpec
	status    string
	substatus string
	messages  []*message
	unread    int
	caps      map[string]int
}

type answer struct {
	text    string
	status  string
	created time.Time
}

type question struct {
	id      string
	seller  string
	item    string
	from    string
	text    string
	status  string
	created time.Time
	answer  *answer
}

type storedAttachment struct {
	name        string
	contentType string
	data        []byte
}

type missedFeed struct {
	notification map[string]any
	topic        string
}

type Server struct {
	cfg           Config
	api           *fixtureSet
	notifications *fixtureSet

	mu          sync.Mutex
	sellers     map[string]*seller
	tokens      map[string]accessToken
	codes       map[string]*authCode
	packs       map[string]*pack
	orders      map[string]string
	shipments   map[string]string
	messages    map[string]string
	questions   map[string]*question
	attachments map[string]storedAttachment
	missed      []missedFeed
	sendPlan    []SendOutcome
	sends       []Sent
	answers     map[string]int
	calls       map[string]int
	defaultUser string

	seq          atomic.Int64
	inFlight     atomic.Int64
	peakInFlight atomic.Int64
	httpSrv      *httptest.Server
	mux          *http.ServeMux
}

func New(cfg Config) *Server {
	if cfg.APIFixtureDir == "" {
		cfg.APIFixtureDir = DefaultAPIFixtureDir()
	}
	if cfg.NotificationFixtureDir == "" {
		cfg.NotificationFixtureDir = DefaultNotificationFixtureDir()
	}
	s := &Server{
		cfg:           cfg,
		api:           newFixtureSet(cfg.APIFixtureDir),
		notifications: newFixtureSet(cfg.NotificationFixtureDir),
		sellers:       map[string]*seller{},
		tokens:        map[string]accessToken{},
		codes:         map[string]*authCode{},
		packs:         map[string]*pack{},
		orders:        map[string]string{},
		shipments:     map[string]string{},
		messages:      map[string]string{},
		questions:     map[string]*question{},
		attachments:   map[string]storedAttachment{},
		answers:       map[string]int{},
		calls:         map[string]int{},
	}
	base := cfg.IDBase
	if base <= 0 {
		base = 13000000000
	}
	s.seq.Store(base)
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) Start() *Server {
	s.httpSrv = httptest.NewServer(s)
	return s
}

func (s *Server) URL() string {
	if s.httpSrv == nil {
		return s.cfg.PublicURL
	}
	return s.httpSrv.URL
}

func (s *Server) AuthorizeURL() string { return s.URL() + "/authorization" }

func (s *Server) Close() {
	if s.httpSrv != nil {
		s.httpSrv.Close()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	handle := func(pattern string, h http.HandlerFunc) {
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.count(pattern)
			current := s.inFlight.Add(1)
			defer s.inFlight.Add(-1)
			for {
				peak := s.peakInFlight.Load()
				if current <= peak || s.peakInFlight.CompareAndSwap(peak, current) {
					break
				}
			}
			h(w, r)
		})
	}
	handle("GET /authorization", s.authorize)
	handle("POST /oauth/token", s.token)
	handle("GET /users/me", s.authed(s.usersMe))
	handle("GET /users/{id}", s.authed(s.user))
	handle("DELETE /users/{id}/applications/{app}", s.authed(s.revoke))
	handle("GET /packs/{id}", s.authed(s.pack))
	handle("GET /orders/{id}", s.authed(s.order))
	handle("GET /shipments/{id}", s.authed(s.shipment))
	handle("GET /messages/packs/{pack}/sellers/{seller}", s.authed(s.thread))
	handle("POST /messages/packs/{pack}/sellers/{seller}", s.authed(s.send))
	handle("GET /messages/unread", s.authed(s.unread))
	handle("GET /messages/{id}", s.authed(s.message))
	handle("POST /messages/attachments", s.authed(s.upload))
	handle("GET /messages/attachments/{id}", s.authed(s.download))
	handle("GET /messages/action_guide/packs/{pack}", s.authed(s.actionGuide))
	handle("GET /messages/action_guide/packs/{pack}/caps_available", s.authed(s.actionCaps))
	handle("POST /messages/action_guide/packs/{pack}/option", s.authed(s.actionOption))
	handle("GET /missed_feeds", s.authed(s.missedFeeds))
	handle("GET /questions/search", s.authed(s.searchQuestions))
	handle("GET /questions/{id}", s.authed(s.question))
	handle("DELETE /questions/{id}", s.authed(s.deleteQuestion))
	handle("POST /answers", s.authed(s.answer))
	handle("GET /users/{id}/questions_blacklist", s.authed(s.blacklist))
	handle("POST /users/{id}/questions_blacklist", s.authed(s.block))
	handle("DELETE /users/{id}/questions_blacklist/{user}", s.authed(s.unblock))
	handle("GET /users/{id}/questions/response_time", s.authed(s.responseTime))
	handle("GET /users/{id}/items/search", s.authed(s.itemSearch))
	handle("GET /items/bulk", s.authed(s.itemsBulk))
	handle("GET "+thumbnailPathBase+"{file}", s.thumbnail)
	s.controlRoutes(handle)
}

func (s *Server) count(pattern string) {
	s.mu.Lock()
	s.calls[pattern]++
	s.mu.Unlock()
}

func (s *Server) Calls() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.calls))
	for k, v := range s.calls {
		out[k] = v
	}
	return out
}

func (s *Server) CallCount(pattern string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[pattern]
}

func (s *Server) TotalCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, v := range s.calls {
		total += v
	}
	return total
}

func (s *Server) ResetCalls() {
	s.mu.Lock()
	s.calls = map[string]int{}
	s.mu.Unlock()
	s.peakInFlight.Store(0)
}

func (s *Server) PeakInFlight() int64 { return s.peakInFlight.Load() }

func (s *Server) AddSeller(spec SellerSpec) {
	if spec.SiteID == "" {
		spec.SiteID = "MLB"
	}
	if spec.Nickname == "" {
		spec.Nickname = "LOJA" + spec.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sellers[spec.ID] = &seller{spec: spec, accessTTL: DefaultAccessTTL}
	if s.defaultUser == "" {
		s.defaultUser = spec.ID
	}
}

func (s *Server) SetDefaultSeller(id string) {
	s.mu.Lock()
	s.defaultUser = id
	s.mu.Unlock()
}

func (s *Server) SetAccessTTL(sellerID string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl, ok := s.sellers[sellerID]; ok {
		sl.accessTTL = ttl
	}
}

func (s *Server) RevokeGrant(sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl, ok := s.sellers[sellerID]; ok {
		sl.revoked = true
		for tok, at := range s.tokens {
			if at.seller == sellerID {
				delete(s.tokens, tok)
			}
		}
	}
}

func (s *Server) RefreshCount(sellerID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl, ok := s.sellers[sellerID]; ok {
		return sl.refreshes
	}
	return 0
}

func (s *Server) IssueCode(sellerID, challenge, redirectURI string) string {
	code := "TG-" + randomHex(12) + "-" + sellerID
	s.mu.Lock()
	s.codes[code] = &authCode{seller: sellerID, challenge: challenge, redirect: redirectURI}
	s.mu.Unlock()
	return code
}

func (s *Server) AddPack(spec PackSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(spec.OrderIDs) == 0 {
		spec.OrderIDs = []string{s.nextIDLocked()}
	}
	if spec.ShipmentID == "" {
		spec.ShipmentID = s.nextIDLocked()
	}
	p := &pack{spec: spec, status: "active", caps: map[string]int{"OTHER": 1, "REQUEST_VARIANTS": 1, "DELIVERY_PROMISE": 1}}
	s.packs[spec.PackID] = p
	for _, o := range spec.OrderIDs {
		s.orders[o] = spec.PackID
	}
	s.shipments[spec.ShipmentID] = spec.PackID
}

func (s *Server) nextIDLocked() string {
	return strconv.FormatInt(s.seq.Add(1), 10)
}

func (s *Server) NextID() string {
	return strconv.FormatInt(s.seq.Add(1), 10)
}

func (s *Server) BlockThread(packID, substatus string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.packs[packID]; ok {
		p.status, p.substatus = "blocked", substatus
	}
}

func (s *Server) BuyerWrites(packID, text string, files ...Attachment) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packs[packID]
	if !ok {
		return ""
	}
	from := p.spec.BuyerID
	if p.spec.Mediated {
		from = AgentUserID
	}
	m := &message{id: randomHex(16), from: from, to: p.spec.SellerID, text: text, status: "available", created: time.Now().UTC()}
	for _, f := range files {
		id := randomHex(8) + "_" + f.FileName
		s.attachments[id] = storedAttachment{name: f.FileName, contentType: f.ContentType, data: f.Data}
		m.attachments = append(m.attachments, id)
	}
	p.messages = append(p.messages, m)
	p.unread++
	s.messages[m.id] = packID
	return m.id
}

func (s *Server) SellerWritesInApp(packID, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packs[packID]
	if !ok {
		return ""
	}
	m := &message{id: randomHex(16), from: p.spec.SellerID, to: p.target(), text: text, status: "available", created: time.Now().UTC()}
	p.messages = append(p.messages, m)
	s.messages[m.id] = packID
	return m.id
}

func (p *pack) target() string {
	if p.spec.Mediated {
		return AgentUserID
	}
	return p.spec.BuyerID
}

func (s *Server) PlanSends(outcomes ...SendOutcome) {
	s.mu.Lock()
	s.sendPlan = append(s.sendPlan, outcomes...)
	s.mu.Unlock()
}

func (s *Server) Sends(packID string) []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Sent
	for _, sent := range s.sends {
		if packID == "" || sent.PackID == packID {
			out = append(out, sent)
		}
	}
	return out
}

func (s *Server) AddItems(sellerID string, items ...ItemSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl, ok := s.sellers[sellerID]; ok {
		for _, it := range items {
			if it.Status == "" {
				it.Status = "active"
			}
			sl.items = append(sl.items, it)
		}
	}
}

func (s *Server) Ask(sellerID, itemID, askerID, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextIDLocked()
	s.questions[id] = &question{id: id, seller: sellerID, item: itemID, from: askerID, text: text, status: "UNANSWERED", created: time.Now().UTC()}
	return id
}

func (s *Server) AnswerInApp(questionID, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, ok := s.questions[questionID]; ok {
		q.status = "ANSWERED"
		q.answer = &answer{text: text, status: "ACTIVE", created: time.Now().UTC()}
	}
}

func (s *Server) QuestionStatus(questionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, ok := s.questions[questionID]; ok {
		return q.status
	}
	return ""
}

func (s *Server) AnswersPosted(questionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answers[questionID]
}

func (s *Server) Blacklisted(sellerID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl, ok := s.sellers[sellerID]
	if !ok {
		return false
	}
	for _, id := range sl.blacklist {
		if id == userID {
			return true
		}
	}
	return false
}

func (s *Server) MissNotification(body []byte) error {
	var n map[string]any
	if err := decodeNumbers(body, &n); err != nil {
		return err
	}
	topic, _ := n["topic"].(string)
	s.mu.Lock()
	s.missed = append(s.missed, missedFeed{notification: n, topic: topic})
	s.mu.Unlock()
	return nil
}

func (s *Server) Notification(topic, sellerID, resource, notificationID string, actions ...string) []byte {
	name := map[string]string{
		"messages":  "notification_messages_doc_example.json",
		"questions": "notification_questions_doc_example.json",
		"orders_v2": "notification_orders_v2_doc_example.json",
		"shipments": "notification_shipments_constructed.json",
		"items":     "notification_items_mlb_from_doc_example.json",
	}[topic]
	if name == "" {
		name = "notification_questions_doc_example.json"
	}
	n := s.notifications.mustObject(name)
	delete(n, "id")
	delete(n, "_id")
	if notificationID != "" {
		n["_id"] = notificationID
	}
	n["resource"] = resource
	n["user_id"] = num(sellerID)
	n["topic"] = topic
	n["application_id"] = num(s.cfg.AppID)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	n["sent"], n["received"] = now, now
	if topic == "messages" {
		if len(actions) == 0 {
			actions = []string{"created"}
		}
		n["actions"] = actions
	}
	raw, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}
	return raw
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"message": message, "error": code, "status": status, "cause": []any{}})
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

type authedHandler func(w http.ResponseWriter, r *http.Request, sellerID string)

func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		s.mu.Lock()
		at, ok := s.tokens[tok]
		s.mu.Unlock()
		if tok == "" || !ok || time.Now().After(at.expires) {
			writeError(w, http.StatusUnauthorized, "invalid_token", "invalid access token")
			return
		}
		h(w, r, at.seller)
	}
}
