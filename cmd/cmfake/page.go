package main

import (
	"html/template"
	"log"
	"net/http"
)

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CM fake</title>
<style>
body { font-family: system-ui, sans-serif; margin: 16px; background: #fafafa; color: #1d1d1f; }
table { border-collapse: collapse; width: 100%; background: #fff; }
th, td { border: 1px solid #ddd; padding: 6px 8px; text-align: left; vertical-align: top; font-size: 14px; }
th { background: #f0f0f0; }
.refused { color: #b00020; }
.reports span { display: inline-block; margin-right: 6px; }
form { display: flex; gap: 4px; margin-top: 4px; }
input[type=text] { flex: 1; min-width: 80px; }
button { cursor: pointer; }
#result { margin: 8px 0; min-height: 1.2em; }
</style>
</head>
<body>
<h1>CM fake</h1>
<p>Phone endings: 0 delivered and read, 1 no RCS (code 42, SMS still delivers), 2 never reported, 3 rejected at once. <button onclick="location.reload()">Refresh</button></p>
<div id="result"></div>
<table>
<thead><tr><th>When</th><th>Channel</th><th>From</th><th>Phone</th><th>Reference</th><th>Text</th><th>Status reports</th><th>Buttons</th><th>Reply</th></tr></thead>
<tbody>
{{range .}}
<tr>
<td>{{.At.Format "15:04:05"}}</td>
<td>{{.Channel}}</td>
<td>{{.From}}</td>
<td>{{.Phone}}</td>
<td><code>{{.Reference}}</code></td>
<td>{{.Text}}{{if not .Accepted}}<div class="refused">rejected {{.ErrorCode}} {{.Detail}}</div>{{end}}</td>
<td class="reports">{{range .Reports}}<span>{{.Status}}{{if .ErrorCode}} ({{.ErrorCode}}){{end}} {{.BackendStatus}}{{if .Error}} {{.Error}}{{end}}</span>{{end}}</td>
<td>{{$send := .}}{{range .Buttons}}<button data-reference="{{$send.Reference}}" data-phone="{{$send.Phone}}" data-postback="{{.Postback}}" data-label="{{.Label}}" onclick="tap(this)">{{.Label}}</button> <small>{{.Postback}}</small><br>{{end}}</td>
<td><form data-reference="{{.Reference}}" data-phone="{{.Phone}}" data-to="{{.From}}" data-channel="{{.Channel}}" onsubmit="return reply(this)"><input type="text" name="text" placeholder="Reply"><button type="submit">Send</button></form></td>
</tr>
{{else}}
<tr><td colspan="9">Nothing sent yet.</td></tr>
{{end}}
</tbody>
</table>
<script>
async function call(path, body) {
  const res = await fetch(path, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
  document.getElementById("result").textContent = path + ": " + res.status + " " + await res.text();
}
function tap(el) {
  call("/_fake/tap", {reference: el.dataset.reference, postback: el.dataset.postback, label: el.dataset.label, phone: el.dataset.phone});
}
function reply(form) {
  const text = form.elements.text.value;
  call("/_fake/inbound", {phone: form.dataset.phone, to: form.dataset.to, channel: form.dataset.channel, text: text, in_reply_to: form.dataset.reference});
  form.elements.text.value = "";
  return false;
}
</script>
</body>
</html>
`))

func (a *app) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, a.snapshot()); err != nil {
		log.Printf("[cmfake] page: %v", err)
	}
}
