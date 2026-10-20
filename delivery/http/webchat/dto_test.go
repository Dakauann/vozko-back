package webchat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	wcdomain "vozko/domain/webchat"
)

func TestAMessageResponseCarriesTheQuotedLineOnlyWhenThereIsOne(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	quoted, _ := json.Marshal(toMessageResponse(wcdomain.VisitorMessage{ID: "m-2", Author: wcdomain.AuthorTeam, Text: "sim", CreatedAt: at, Quote: &wcdomain.VisitorQuote{ID: "m-1", Text: "tem estoque?"}}))
	if !strings.Contains(string(quoted), `"quote":{"id":"m-1","text":"tem estoque?"}`) {
		t.Fatalf("response = %s", quoted)
	}
	plain, _ := json.Marshal(toMessageResponse(wcdomain.VisitorMessage{ID: "m-3", Author: wcdomain.AuthorTeam, Text: "oi", CreatedAt: at}))
	if strings.Contains(string(plain), "quote") {
		t.Fatalf("a message without a quote must not carry the field: %s", plain)
	}
}
