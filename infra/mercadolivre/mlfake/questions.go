package mlfake

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) questionFor(w http.ResponseWriter, id, sellerID string) (*question, bool) {
	q, ok := s.questions[id]
	if !ok || q.status == "DELETED" {
		writeError(w, http.StatusNotFound, "not_found", "question "+id+" not found")
		return nil, false
	}
	if q.seller != sellerID {
		writeError(w, http.StatusForbidden, "forbidden", "the question belongs to another seller")
		return nil, false
	}
	return q, true
}

func answerJSON(q *question) any {
	if q.answer == nil {
		return nil
	}
	return map[string]any{"text": q.answer.text, "status": q.answer.status, "date_created": stamp(q.answer.created)}
}

func (s *Server) questionV4(q *question) map[string]any {
	out := s.api.mustObject("question_v4_doc_example.json")
	out["id"] = num(q.id)
	out["seller_id"] = num(q.seller)
	out["buyer_id"] = num(q.from)
	out["item_id"] = q.item
	out["status"] = q.status
	out["text"] = q.text
	out["app_id"] = num(s.cfg.AppID)
	out["date_created"] = stamp(q.created)
	out["last_updated"] = stamp(time.Now())
	out["answer"] = answerJSON(q)
	return out
}

func (s *Server) question(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questionFor(w, r.PathValue("id"), sellerID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.questionV4(q))
}

func (s *Server) searchQuestions(w http.ResponseWriter, r *http.Request, sellerID string) {
	params := r.URL.Query()
	if params.Get("seller_id") != sellerID {
		writeError(w, http.StatusForbidden, "forbidden", "seller_id does not match the token")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sl := s.sellers[sellerID]; sl != nil && sl.spec.DeniedPermission {
		writeError(w, http.StatusForbidden, "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", "At least one policy returned UNAUTHORIZED.")
		return
	}
	offset, _ := strconv.Atoi(params.Get("offset"))
	limit, _ := strconv.Atoi(params.Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	var matching []*question
	for _, q := range s.questions {
		if q.seller != sellerID || q.status == "DELETED" {
			continue
		}
		if st := params.Get("status"); st != "" && q.status != st {
			continue
		}
		if item := params.Get("item"); item != "" && q.item != item {
			continue
		}
		matching = append(matching, q)
	}
	sort.Slice(matching, func(i, j int) bool {
		if matching[i].created.Equal(matching[j].created) {
			return matching[i].id > matching[j].id
		}
		return matching[i].created.After(matching[j].created)
	})
	body := s.api.mustObject("questions_search_doc_example.json")
	template := firstObject(body["questions"])
	page := []any{}
	for i := offset; i < len(matching) && len(page) < limit; i++ {
		q := matching[i]
		entry := clone(template)
		entry["id"] = num(q.id)
		entry["seller_id"] = num(q.seller)
		entry["item_id"] = q.item
		entry["status"] = q.status
		entry["text"] = q.text
		entry["date_created"] = stamp(q.created)
		entry["answer"] = answerJSON(q)
		entry["from"] = map[string]any{"id": num(q.from)}
		page = append(page, entry)
	}
	body["total"] = len(matching)
	body["limit"] = limit
	body["questions"] = page
	filters := child(body, "filters")
	filters["offset"], filters["limit"] = offset, limit
	filters["caller"], filters["seller"] = num(sellerID), num(sellerID)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, sellerID string) {
	var body struct {
		QuestionID json.Number `json:"question_id"`
		Text       string      `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unreadable body")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questionFor(w, body.QuestionID.String(), sellerID)
	if !ok {
		return
	}
	if q.status != "UNANSWERED" {
		writeError(w, http.StatusBadRequest, "bad_request", "Question "+q.id+" is not unanswered, it cannot be answered")
		return
	}
	if strings.TrimSpace(body.Text) == "" || len([]rune(body.Text)) > 2000 {
		writeError(w, http.StatusBadRequest, "bad_request", "answer text must have 1 to 2000 characters")
		return
	}
	q.status = "ANSWERED"
	q.answer = &answer{text: body.Text, status: "ACTIVE", created: time.Now().UTC()}
	s.answers[q.id]++
	out := s.api.mustObject("answer_doc_example.json")
	out["id"] = num(q.id)
	out["answer"] = answerJSON(q)
	out["date_created"] = stamp(q.created)
	out["item_id"] = q.item
	out["seller_id"] = num(q.seller)
	out["status"] = q.status
	out["text"] = q.text
	child(out, "from")["id"] = num(q.from)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteQuestion(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questionFor(w, r.PathValue("id"), sellerID)
	if !ok {
		return
	}
	q.status = "DELETED"
	writeJSON(w, http.StatusOK, "Question deleted.")
}

func (s *Server) blacklist(w http.ResponseWriter, r *http.Request, sellerID string) {
	if ownerMismatch(w, r.PathValue("id"), sellerID) {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sellers[sellerID]
	body := s.api.mustObject("questions_blacklist_doc_example.json")
	users := []any{}
	for i := offset; i < len(sl.blacklist) && len(users) < limit; i++ {
		users = append(users, map[string]any{"id": num(sl.blacklist[i])})
	}
	body["users"] = users
	body["paging"] = map[string]any{"total": len(sl.blacklist), "limit": limit, "offset": offset}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) block(w http.ResponseWriter, r *http.Request, sellerID string) {
	if ownerMismatch(w, r.PathValue("id"), sellerID) {
		return
	}
	var body struct {
		UserID json.Number `json:"user_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || body.UserID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "user_id is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sellers[sellerID]
	for _, id := range sl.blacklist {
		if id == body.UserID.String() {
			writeJSON(w, http.StatusOK, map[string]any{"user_id": body.UserID})
			return
		}
	}
	sl.blacklist = append(sl.blacklist, body.UserID.String())
	writeJSON(w, http.StatusOK, map[string]any{"user_id": body.UserID})
}

func (s *Server) unblock(w http.ResponseWriter, r *http.Request, sellerID string) {
	if ownerMismatch(w, r.PathValue("id"), sellerID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sellers[sellerID]
	kept := sl.blacklist[:0]
	for _, id := range sl.blacklist {
		if id != r.PathValue("user") {
			kept = append(kept, id)
		}
	}
	sl.blacklist = kept
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) responseTime(w http.ResponseWriter, r *http.Request, sellerID string) {
	if ownerMismatch(w, r.PathValue("id"), sellerID) {
		return
	}
	body := s.api.mustObject("response_time_doc_example.json")
	body["user_id"] = num(sellerID)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) itemSearch(w http.ResponseWriter, r *http.Request, sellerID string) {
	if ownerMismatch(w, r.PathValue("id"), sellerID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.sellers[sellerID]
	body := s.api.mustObject("items_search_doc_example.json")
	results := []any{}
	if r.URL.Query().Get("scroll_id") == "" {
		for _, it := range sl.items {
			results = append(results, it.ID)
		}
	}
	body["seller_id"] = sellerID
	body["results"] = results
	body["paging"] = map[string]any{"limit": 100, "offset": 0, "total": len(sl.items)}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) itemsBulk(w http.ResponseWriter, r *http.Request, sellerID string) {
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(ids) == 0 || len(ids) > 20 {
		writeError(w, http.StatusBadRequest, "bad_request", "ids must list 1 to 20 items")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.api.mustList("items_bulk_from_multiget_doc_example.json")
	found := firstObject(entries)
	missing := clone(entries[len(entries)-1].(map[string]any))
	known := map[string]ItemSpec{}
	for _, sl := range s.sellers {
		for _, it := range sl.items {
			known[it.ID] = it
		}
	}
	for _, p := range s.packs {
		for _, it := range p.spec.Items {
			if _, ok := known[it.ItemID]; !ok {
				known[it.ItemID] = ItemSpec{ID: it.ItemID, Title: it.Title, Status: "active"}
			}
		}
	}
	out := []any{}
	for _, id := range ids {
		it, ok := known[id]
		if !ok {
			entry := clone(missing)
			entry["id"] = id
			child(entry, "body")["message"] = "Item with id " + id + " not found"
			out = append(out, entry)
			continue
		}
		entry := clone(found)
		entry["id"] = id
		body := child(entry, "body")
		body["id"] = id
		body["title"] = it.Title
		body["seller_id"] = num(sellerID)
		body["status"] = it.Status
		body["permalink"] = "https://produto.mercadolivre.com.br/" + strings.Replace(id, "MLB", "MLB-", 1)
		body["thumbnail"] = s.URL() + thumbnailPathBase + id + ".jpg"
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

var tinyJPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
	0xff, 0xdb, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0a, 0x0c,
	0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12, 0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
	0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29, 0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27,
	0x39, 0x3d, 0x38, 0x32, 0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01,
	0x11, 0x00, 0xff, 0xc4, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x09, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00, 0x54, 0xbf,
	0xff, 0xd9,
}

func (s *Server) thumbnail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(tinyJPEG)
}
