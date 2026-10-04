package marketing

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func pageLinkGraph(answer string) func(call recordedCall) (int, string) {
	return func(call recordedCall) (int, string) {
		if call.method == http.MethodGet {
			return http.StatusOK, `{"access_token":"page-tok"}`
		}
		return http.StatusOK, answer
	}
}

func TestAskingForTheLinkCodeSendsOnlyTheNumber(t *testing.T) {
	g, calls := gatewayWith(t, pageLinkGraph(`{"verification_status":"VERIFICATION_CODE_SEND_SUCCESS"}`))
	status, err := g.RequestPageNumberCode(context.Background(), "tok", "1348090328393443", "5511965467700")
	if err != nil || status != "VERIFICATION_CODE_SEND_SUCCESS" {
		t.Fatalf("status %q err %v", status, err)
	}
	post := (*calls)[len(*calls)-1]
	if post.method != http.MethodPost || post.path != "/v26.0/1348090328393443/page_whatsapp_number_verification" {
		t.Fatalf("call %+v", post)
	}
	if post.form.Get("whatsapp_number") != "5511965467700" || post.form.Has("verification_code") {
		t.Fatalf("form %v", post.form)
	}
}

func TestVerifyingTheLinkSendsTheNumberAndTheCode(t *testing.T) {
	g, calls := gatewayWith(t, pageLinkGraph(`{"verification_status":"VERIFIED"}`))
	status, err := g.VerifyPageNumber(context.Background(), "tok", "1348090328393443", "5511965467700", "83569")
	if err != nil || status != "VERIFIED" {
		t.Fatalf("status %q err %v", status, err)
	}
	post := (*calls)[len(*calls)-1]
	if post.form.Get("whatsapp_number") != "5511965467700" || post.form.Get("verification_code") != "83569" {
		t.Fatalf("form %v", post.form)
	}
}

func TestMetaLinkErrorMessageIsKept(t *testing.T) {
	g, _ := gatewayWith(t, pageLinkGraph(`{"verification_status":"","error_message":"Código inválido"}`))
	status, err := g.VerifyPageNumber(context.Background(), "tok", "1348090328393443", "5511965467700", "11111")
	if err == nil || status != "" {
		t.Fatalf("status %q err %v", status, err)
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "Código inválido") {
		t.Fatalf("error %q", got)
	}
}

func TestPagesCarryTheirBusiness(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"id":"P1","name":"Loja","business":{"id":"B1","name":"Loja SA"}}]}`))
	pages, err := g.ListPages(context.Background(), "tok")
	if err != nil || len(pages) != 1 || pages[0].BusinessID != "B1" {
		t.Fatalf("pages %+v err %v", pages, err)
	}
	if !strings.Contains((*calls)[0].query.Get("fields"), "business{id}") {
		t.Fatalf("fields %s", (*calls)[0].query.Get("fields"))
	}
}
