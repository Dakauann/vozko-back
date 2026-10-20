package mlfake

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.cfg.AppID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		writeError(w, http.StatusBadRequest, "invalid_request", "unknown client or missing pkce")
		return
	}
	sellerID := q.Get("seller")
	s.mu.Lock()
	if sellerID == "" {
		sellerID = s.defaultUser
	}
	sl, ok := s.sellers[sellerID]
	s.mu.Unlock()
	target := q.Get("redirect_uri")
	if s.cfg.CallbackOverride != "" {
		target = s.cfg.CallbackOverride
	}
	back, err := url.Parse(target)
	if err != nil || !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "unknown seller or redirect uri")
		return
	}
	params := back.Query()
	params.Set("state", q.Get("state"))
	if sl.spec.Operator {
		params.Set("error", "invalid_operator_user_id")
		params.Set("error_description", "invalid_operator_user_id")
	} else {
		params.Set("code", s.IssueCode(sellerID, q.Get("code_challenge"), q.Get("redirect_uri")))
	}
	back.RawQuery = params.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "unreadable form")
		return
	}
	f := r.PostForm
	if f.Get("client_id") != s.cfg.AppID || f.Get("client_secret") != s.cfg.ClientSecret {
		writeError(w, http.StatusUnauthorized, "invalid_client", "invalid client_id or client_secret")
		return
	}
	switch f.Get("grant_type") {
	case "authorization_code":
		s.exchange(w, f)
	case "refresh_token":
		s.refresh(w, f)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant type")
	}
}

func (s *Server) exchange(w http.ResponseWriter, f url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[f.Get("code")]
	if !ok || code.used {
		writeError(w, http.StatusBadRequest, "invalid_grant", "Error validating grant. Your authorization code or refresh token may be expired or it was already used")
		return
	}
	if code.redirect != f.Get("redirect_uri") {
		writeError(w, http.StatusBadRequest, "invalid_grant", "the redirect_uri does not match the authorization request")
		return
	}
	if code.challenge == "" || pkceChallenge(f.Get("code_verifier")) != code.challenge {
		writeError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match the code_challenge")
		return
	}
	code.used = true
	sl := s.sellers[code.seller]
	if sl.spec.Operator {
		writeError(w, http.StatusForbidden, "invalid_operator_user_id", "invalid_operator_user_id")
		return
	}
	sl.revoked = false
	writeJSON(w, http.StatusOK, s.grantLocked(sl))
}

func (s *Server) refresh(w http.ResponseWriter, f url.Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	presented := f.Get("refresh_token")
	var owner *seller
	for _, sl := range s.sellers {
		if sl.refreshToken != "" && sl.refreshToken == presented {
			owner = sl
			break
		}
	}
	if owner == nil || owner.revoked {
		writeError(w, http.StatusBadRequest, "invalid_grant", "Error validating grant. Your authorization code or refresh token may be expired or it was already used")
		return
	}
	owner.refreshes++
	writeJSON(w, http.StatusOK, s.grantLocked(owner))
}

func (s *Server) grantLocked(sl *seller) map[string]any {
	access := "APP_USR-" + randomHex(10) + "-" + sl.spec.ID
	sl.refreshToken = "TG-" + randomHex(12) + "-" + sl.spec.ID
	ttl := sl.accessTTL
	s.tokens[access] = accessToken{seller: sl.spec.ID, expires: time.Now().Add(ttl)}
	return map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int64(ttl / time.Second),
		"scope":         grantedScope,
		"user_id":       num(sl.spec.ID),
		"refresh_token": sl.refreshToken,
	}
}

func (s *Server) usersMe(w http.ResponseWriter, r *http.Request, sellerID string) {
	s.mu.Lock()
	sl := s.sellers[sellerID]
	s.mu.Unlock()
	user := s.api.mustObject("users_me_doc_example_trimmed.json")
	user["id"] = num(sellerID)
	user["nickname"] = sl.spec.Nickname
	user["site_id"] = sl.spec.SiteID
	user["country_id"] = map[string]string{"MLB": "BR", "MLA": "AR"}[sl.spec.SiteID]
	user["permalink"] = "http://perfil.mercadolivre.com.br/" + sl.spec.Nickname
	user["user_type"] = "normal"
	user["tags"] = []string{"normal", "test_user"}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")
	user := s.api.mustObject("users_me_doc_example_trimmed.json")
	for _, private := range []string{"email", "first_name", "last_name", "registration_date"} {
		delete(user, private)
	}
	nickname := "COMPRADOR" + id
	s.mu.Lock()
	if sl, ok := s.sellers[id]; ok {
		nickname = sl.spec.Nickname
	}
	s.mu.Unlock()
	if id == AgentUserID {
		nickname = "MERCADOLIVRE_AGENTE"
	}
	user["id"] = num(id)
	user["nickname"] = nickname
	user["site_id"] = "MLB"
	user["country_id"] = "BR"
	user["user_type"] = "normal"
	user["permalink"] = "http://perfil.mercadolivre.com.br/" + nickname
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request, sellerID string) {
	if r.PathValue("id") != sellerID || r.PathValue("app") != s.cfg.AppID {
		writeError(w, http.StatusForbidden, "forbidden", "not your application grant")
		return
	}
	s.RevokeGrant(sellerID)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": num(sellerID), "app_id": num(s.cfg.AppID), "msg": "Autorização excluída"})
}

func ownerMismatch(w http.ResponseWriter, pathSeller, sellerID string) bool {
	if strings.TrimSpace(pathSeller) != sellerID {
		writeError(w, http.StatusForbidden, "forbidden", "the token does not belong to this seller")
		return true
	}
	return false
}
