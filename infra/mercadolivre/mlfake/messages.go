package mlfake

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) packFor(w http.ResponseWriter, packID, sellerID string) (*pack, bool) {
	p, ok := s.packs[packID]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "pack "+packID+" not found")
		return nil, false
	}
	if p.spec.SellerID != sellerID {
		writeError(w, http.StatusForbidden, "forbidden", "the pack belongs to another seller")
		return nil, false
	}
	return p, true
}

func (s *Server) pack(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packFor(w, r.PathValue("id"), sellerID)
	if !ok {
		return
	}
	body := s.api.mustObject("pack_doc_example.json")
	body["id"] = num(p.spec.PackID)
	child(body, "buyer")["id"] = num(p.spec.BuyerID)
	child(body, "shipment")["id"] = num(p.spec.ShipmentID)
	orders := make([]any, 0, len(p.spec.OrderIDs))
	for _, o := range p.spec.OrderIDs {
		orders = append(orders, map[string]any{"id": num(o)})
	}
	body["orders"] = orders
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) order(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	orderID := r.PathValue("id")
	packID, ok := s.orders[orderID]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "order "+orderID+" not found")
		return
	}
	p, ok := s.packFor(w, packID, sellerID)
	if !ok {
		return
	}
	body := s.api.mustObject("order_doc_example_cleaned.json")
	template := firstObject(body["order_items"])
	items := make([]any, 0, len(p.spec.Items))
	total := 0.0
	for _, it := range p.spec.Items {
		line := clone(template)
		item := child(line, "item")
		item["id"], item["title"] = it.ItemID, it.Title
		line["quantity"] = it.Quantity
		line["unit_price"] = it.UnitPrice
		line["full_unit_price"] = it.UnitPrice
		line["currency_id"] = "BRL"
		total += float64(it.Quantity) * it.UnitPrice
		items = append(items, line)
	}
	body["id"] = num(orderID)
	body["pack_id"] = num(p.spec.PackID)
	if len(p.spec.OrderIDs) == 1 && p.spec.OrderIDs[0] == p.spec.PackID {
		body["pack_id"] = nil
	}
	body["order_items"] = items
	body["total_amount"], body["paid_amount"] = total, total
	child(body, "buyer")["id"] = num(p.spec.BuyerID)
	child(body, "seller")["id"] = num(p.spec.SellerID)
	child(body, "shipping")["id"] = num(p.spec.ShipmentID)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) shipment(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	packID, ok := s.shipments[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "shipment "+id+" not found")
		return
	}
	if _, ok := s.packFor(w, packID, sellerID); !ok {
		return
	}
	body := s.api.mustObject("shipment_from_doc_schema.json")
	body["id"] = num(id)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) threadPath(p *pack) string {
	path := "/packs/" + p.spec.PackID + "/sellers/" + p.spec.SellerID
	if p.spec.Mediated {
		path += mediatedPathTail
	}
	return path
}

func (s *Server) messageJSON(p *pack, m *message) map[string]any {
	thread := s.api.mustObject("thread_doc_example.json")
	out := firstObject(thread["messages"])
	out["id"] = m.id
	out["site_id"] = "MLB"
	out["client_id"] = num(s.cfg.AppID)
	out["from"] = map[string]any{"user_id": num(m.from)}
	out["to"] = map[string]any{"user_id": num(m.to)}
	out["status"] = m.status
	out["text"] = m.text
	dates := child(out, "message_date")
	created := stamp(m.created)
	dates["received"], dates["available"], dates["notified"], dates["created"] = created, created, created, created
	dates["read"] = nil
	if m.read != nil {
		dates["read"] = stamp(*m.read)
	}
	moderation := child(out, "message_moderation")
	moderation["moderation_date"] = created
	if m.status == "pending_review" {
		moderation["status"] = "pending"
	}
	if len(m.attachments) == 0 {
		out["message_attachments"] = nil
	} else {
		list := make([]any, 0, len(m.attachments))
		for _, id := range m.attachments {
			a := s.attachments[id]
			list = append(list, map[string]any{
				"filename": id, "original_filename": a.name, "type": a.contentType, "size": len(a.data),
				"date_created": created, "potential_security_threat": false,
			})
		}
		out["message_attachments"] = list
	}
	out["message_resources"] = []any{
		map[string]any{"id": p.spec.PackID, "name": "packs"},
		map[string]any{"id": p.spec.SellerID, "name": "sellers"},
	}
	out["conversation_id"] = s.threadPath(p)
	return out
}

func (s *Server) message(w http.ResponseWriter, r *http.Request, sellerID string) {
	if r.URL.Query().Get("tag") != "post_sale" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag post_sale is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	packID, ok := s.messages[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "message "+id+" not found")
		return
	}
	p, ok := s.packFor(w, packID, sellerID)
	if !ok {
		return
	}
	var m *message
	for _, candidate := range p.messages {
		if candidate.id == id {
			m = candidate
		}
	}
	if r.URL.Query().Get("mark_as_read") != "false" && m.from != sellerID && m.read == nil {
		now := time.Now().UTC()
		m.read = &now
	}
	body := s.api.mustObject("message_new_format_doc_example.json")
	body["messages"] = []any{s.messageJSON(p, m)}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) thread(w http.ResponseWriter, r *http.Request, sellerID string) {
	q := r.URL.Query()
	if q.Get("tag") != "post_sale" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag post_sale is required")
		return
	}
	if ownerMismatch(w, r.PathValue("seller"), sellerID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packFor(w, r.PathValue("pack"), sellerID)
	if !ok {
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	fixture := "thread_doc_example.json"
	if p.status == "blocked" {
		fixture = "thread_blocked_doc_example.json"
	}
	body := s.api.mustObject(fixture)
	cs := child(body, "conversation_status")
	cs["path"] = s.threadPath(p)
	cs["status"] = p.status
	if p.substatus == "" {
		cs["substatus"] = nil
	} else {
		cs["substatus"] = p.substatus
	}
	cs["status_date"] = stamp(time.Now())
	newestFirst := make([]*message, 0, len(p.messages))
	for i := len(p.messages) - 1; i >= 0; i-- {
		newestFirst = append(newestFirst, p.messages[i])
	}
	page := []any{}
	for i := offset; i < len(newestFirst) && len(page) < limit; i++ {
		page = append(page, s.messageJSON(p, newestFirst[i]))
	}
	if q.Get("mark_as_read") != "false" {
		p.unread = 0
	}
	body["messages"] = page
	body["paging"] = map[string]any{"limit": limit, "offset": offset, "total": len(p.messages)}
	body["seller_max_message_length"] = 350
	body["buyer_max_message_length"] = 3500
	writeJSON(w, http.StatusOK, body)
}

type sendWire struct {
	From struct {
		UserID string `json:"user_id"`
	} `json:"from"`
	To struct {
		UserID string `json:"user_id"`
	} `json:"to"`
	Text        string   `json:"text"`
	Attachments []string `json:"attachments"`
}

func (s *Server) send(w http.ResponseWriter, r *http.Request, sellerID string) {
	if r.URL.Query().Get("tag") != "post_sale" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag post_sale is required")
		return
	}
	if ownerMismatch(w, r.PathValue("seller"), sellerID) {
		return
	}
	var body sendWire
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unreadable body")
		return
	}
	s.mu.Lock()
	p, ok := s.packFor(w, r.PathValue("pack"), sellerID)
	if !ok {
		s.mu.Unlock()
		return
	}
	outcome := SendAccepted
	if len(s.sendPlan) > 0 {
		outcome, s.sendPlan = s.sendPlan[0], s.sendPlan[1:]
	}
	sent := Sent{PackID: p.spec.PackID, From: body.From.UserID, To: body.To.UserID, Text: body.Text, Attachments: body.Attachments, Outcome: outcome}
	s.sends = append(s.sends, sent)
	if body.From.UserID != sellerID || body.To.UserID != p.target() {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "bad_request", "invalid receiver for this conversation")
		return
	}
	if len([]rune(body.Text)) > 350 {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "bad_request", "text is longer than 350 characters")
		return
	}
	if p.status == "blocked" {
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "forbidden", "Conversation blocked: "+p.substatus)
		return
	}
	switch outcome {
	case SendBlockedByMediation:
		p.status, p.substatus = "blocked", "blocked_by_mediation"
		s.mu.Unlock()
		writeError(w, http.StatusForbidden, "forbidden", "The message could not be sent because a mediation is in process")
		return
	case SendRateLimited:
		s.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "local_rate_limited", "Too many requests")
		return
	case SendConnectionDropped:
		s.mu.Unlock()
		dropConnection(w)
		return
	}
	m := &message{id: randomHex(16), from: sellerID, to: body.To.UserID, text: body.Text, status: "available", created: time.Now().UTC(), attachments: body.Attachments}
	status := http.StatusCreated
	if outcome == SendPendingReview {
		m.status = "pending_review"
		status = http.StatusAccepted
	}
	p.messages = append(p.messages, m)
	s.messages[m.id] = p.spec.PackID
	out := s.api.mustObject("option_sent_doc_example.json")
	out["id"] = m.id
	child(out, "to")["user_id"] = num(m.to)
	out["status"] = m.status
	out["text"] = m.text
	dates := child(out, "message_date")
	created := stamp(m.created)
	dates["received"], dates["available"], dates["created"] = created, created, created
	if m.status == "pending_review" {
		child(out, "message_moderation")["status"] = "pending"
	}
	s.mu.Unlock()
	writeJSON(w, status, out)
}

func dropConnection(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	conn, _, err := hj.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func (s *Server) unread(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl := s.sellers[sellerID]; sl != nil && sl.spec.DeniedPermission {
		writeError(w, http.StatusForbidden, "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", "At least one policy returned UNAUTHORIZED.")
		return
	}
	body := s.api.mustObject("unread_doc_example.json")
	body["user_id"] = num(sellerID)
	results := []any{}
	for _, id := range sortedKeys(s.packs) {
		p := s.packs[id]
		if p.spec.SellerID == sellerID && p.unread > 0 {
			results = append(results, map[string]any{"resource": "/packs/" + p.spec.PackID + "/sellers/" + sellerID, "count": p.unread})
		}
	}
	body["results"] = results
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, _ string) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		writeError(w, http.StatusBadRequest, "bad_request", "multipart file expected")
		return
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		writeError(w, http.StatusBadRequest, "bad_request", "file part expected")
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, 25<<20+1))
	if err != nil || len(data) > 25<<20 {
		writeError(w, http.StatusBadRequest, "bad_request", "file too large")
		return
	}
	id := randomHex(8) + "_" + part.FileName()
	s.mu.Lock()
	s.attachments[id] = storedAttachment{name: part.FileName(), contentType: part.Header.Get("Content-Type"), data: data}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, _ string) {
	s.mu.Lock()
	a, ok := s.attachments[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "attachment not found")
		return
	}
	w.Header().Set("Content-Type", a.contentType)
	_, _ = w.Write(a.data)
}

func (s *Server) actionGuide(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packFor(w, r.PathValue("pack"), sellerID)
	if !ok {
		return
	}
	body := s.api.mustObject("action_guide_options_doc_example_fixed.json")
	if options, ok := body["options"].([]any); ok {
		for _, raw := range options {
			if o, ok := raw.(map[string]any); ok {
				id, _ := o["id"].(string)
				o["cap_available"] = p.caps[id]
			}
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) actionCaps(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packFor(w, r.PathValue("pack"), sellerID)
	if !ok {
		return
	}
	caps := s.api.mustList("action_guide_caps_doc_example.json")
	for _, raw := range caps {
		if c, ok := raw.(map[string]any); ok {
			id, _ := c["option_id"].(string)
			c["cap_available"] = p.caps[id]
		}
	}
	writeJSON(w, http.StatusOK, caps)
}

func (s *Server) actionOption(w http.ResponseWriter, r *http.Request, sellerID string) {
	var body struct {
		OptionID   string `json:"option_id"`
		TemplateID string `json:"template_id"`
		Text       string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unreadable body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packFor(w, r.PathValue("pack"), sellerID)
	if !ok {
		return
	}
	if p.caps[body.OptionID] <= 0 {
		writeError(w, http.StatusBadRequest, "cap_exceeded", "The option has no cap available")
		return
	}
	p.caps[body.OptionID]--
	text := body.Text
	if text == "" {
		text = body.TemplateID
	}
	m := &message{id: randomHex(16), from: sellerID, to: p.spec.BuyerID, text: text, status: "available", created: time.Now().UTC()}
	p.messages = append(p.messages, m)
	s.messages[m.id] = p.spec.PackID
	out := s.api.mustObject("option_sent_doc_example.json")
	out["id"] = m.id
	child(out, "to")["user_id"] = num(m.to)
	out["text"] = text
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) missedFeeds(w http.ResponseWriter, r *http.Request, _ string) {
	q := r.URL.Query()
	if q.Get("app_id") != s.cfg.AppID {
		writeError(w, http.StatusForbidden, "forbidden", "app_id does not match the token application")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	template := firstObject(s.api.mustObject("missed_feeds_doc_example_fixed.json")["messages"])
	var matching []any
	for _, m := range s.missed {
		if topic := q.Get("topic"); topic != "" && topic != m.topic {
			continue
		}
		entry := clone(template)
		for k, v := range m.notification {
			entry[k] = v
		}
		raw, _ := json.Marshal(m.notification)
		child(entry, "request")["data"] = string(raw)
		matching = append(matching, entry)
	}
	page := []any{}
	for i := offset; i < len(matching) && len(page) < limit; i++ {
		page = append(page, matching[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": page})
}
