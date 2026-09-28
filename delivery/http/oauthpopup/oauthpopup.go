package oauthpopup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"vozko/delivery/http/response"
	"vozko/usecases/shared/oauthstate"
)

func WriteResult(w http.ResponseWriter, frontendOrigin string, payload map[string]any) {
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to render result", nil)
		return
	}
	target := strings.TrimRight(strings.TrimSpace(frontendOrigin), "/")
	if target == "" {
		target = "null"
	}
	encodedTarget, err := json.Marshal(target)
	if err != nil {
		encodedTarget = []byte(`"null"`)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Vozko</title></head>
<body style="font:14px system-ui;padding:24px;text-align:center;color:#444">
<p>You can close this window.</p>
<script>
(function () {
  var payload = %s;
  var target = %s;
  try {
    if (window.opener && target !== "null") {
      window.opener.postMessage(payload, target);
    }
  } catch (e) {}
  setTimeout(function () { try { window.close(); } catch (e) {} }, 300);
})();
</script>
</body></html>`, encodedPayload, encodedTarget)
}

func Redirect(w http.ResponseWriter, r *http.Request, frontendBaseURL, path, fallbackPath string, params url.Values) {
	target := strings.TrimRight(frontendBaseURL, "/") + oauthstate.SafeReturnPath(path, fallbackPath)
	parsed, err := url.Parse(target)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Invalid redirect target", nil)
		return
	}
	q := parsed.Query()
	for key, values := range params {
		for _, v := range values {
			if v != "" {
				q.Add(key, v)
			}
		}
	}
	parsed.RawQuery = q.Encode()
	http.Redirect(w, r, parsed.String(), http.StatusFound)
}
