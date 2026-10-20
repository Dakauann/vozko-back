package mlfake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (s *Server) controlRoutes(handle func(string, http.HandlerFunc)) {
	handle("POST /_fake/sellers", s.controlSeller)
	handle("POST /_fake/default-seller", s.controlDefaultSeller)
	handle("POST /_fake/items", s.controlItems)
	handle("POST /_fake/packs", s.controlPack)
	handle("POST /_fake/buyer-message", s.controlBuyerMessage)
	handle("POST /_fake/questions", s.controlQuestion)
	handle("GET /_fake/sends", s.controlSends)
	handle("GET /_fake/calls", s.controlCalls)
}

func decodeControl(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func (s *Server) controlSeller(w http.ResponseWriter, r *http.Request) {
	var spec SellerSpec
	if !decodeControl(w, r, &spec) {
		return
	}
	s.AddSeller(spec)
	writeJSON(w, http.StatusOK, spec)
}

func (s *Server) controlDefaultSeller(w http.ResponseWriter, r *http.Request) {
	var body struct{ ID string }
	if !decodeControl(w, r, &body) {
		return
	}
	s.SetDefaultSeller(body.ID)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) controlItems(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SellerID string
		Items    []ItemSpec
	}
	if !decodeControl(w, r, &body) {
		return
	}
	s.AddItems(body.SellerID, body.Items...)
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) controlPack(w http.ResponseWriter, r *http.Request) {
	var spec PackSpec
	if !decodeControl(w, r, &spec) {
		return
	}
	s.AddPack(spec)
	writeJSON(w, http.StatusOK, spec)
}

func (s *Server) controlBuyerMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PackID string
		Text   string
	}
	if !decodeControl(w, r, &body) {
		return
	}
	s.mu.Lock()
	p, ok := s.packs[body.PackID]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "unknown pack")
		return
	}
	id := s.BuyerWrites(body.PackID, body.Text)
	status, err := s.Deliver(s.Notification("messages", p.spec.SellerID, id, "fake-"+id))
	writeJSON(w, http.StatusOK, map[string]any{"messageId": id, "webhookStatus": status, "webhookError": errString(err)})
}

func (s *Server) controlQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SellerID string
		ItemID   string
		AskerID  string
		Text     string
	}
	if !decodeControl(w, r, &body) {
		return
	}
	id := s.Ask(body.SellerID, body.ItemID, body.AskerID, body.Text)
	status, err := s.Deliver(s.Notification("questions", body.SellerID, "/questions/"+id, "fake-q-"+id))
	writeJSON(w, http.StatusOK, map[string]any{"questionId": id, "webhookStatus": status, "webhookError": errString(err)})
}

func (s *Server) controlSends(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Sends(r.URL.Query().Get("pack")))
}

func (s *Server) controlCalls(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Calls())
}

func (s *Server) Deliver(body []byte) (int, error) {
	if s.cfg.WebhookURL == "" {
		return 0, fmt.Errorf("mlfake: no webhook url configured")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(s.cfg.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
