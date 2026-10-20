package conversation

import (
	"strings"
	"testing"
)

func TestNewSendRequestOnlyDeduplicatesWhenTheClientSentAnID(t *testing.T) {
	cases := map[string]struct {
		requestID string
		wantOK    bool
		wantErr   error
	}{
		"a client request id":            {"req-1", true, nil},
		"no request id from old clients": {"", false, nil},
		"only spaces":                    {"   ", false, nil},
		"an id longer than the column":   {strings.Repeat("r", MaxClientRequestIDLength+1), false, ErrClientRequestIDInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			request, ok, err := NewSendRequest("whatsapp", " entry-1 ", tc.requestID)
			if ok != tc.wantOK || err != tc.wantErr {
				t.Fatalf("ok = %v err = %v, want %v %v", ok, err, tc.wantOK, tc.wantErr)
			}
			if ok && (request.EntryID != "entry-1" || request.RequestID != strings.TrimSpace(tc.requestID) || request.EntryType != "whatsapp") {
				t.Fatalf("request = %+v", request)
			}
		})
	}
}
