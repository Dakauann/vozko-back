package googlesheets

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/googlesheet"
)

type recorded struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func fakeGoogle(t *testing.T, respond func(r *http.Request) (int, string)) (*Client, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	calls := []recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, recorded{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery, body: body})
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		status, payload := respond(r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	return New(Endpoints{Sheets: server.URL + "/v4/spreadsheets", Drive: server.URL + "/drive/v3/files"}), &calls
}

func columns() []googlesheet.Column {
	return []googlesheet.Column{
		{Key: "nome", Header: "Nome", Type: googlesheet.TypeText, Source: googlesheet.SourceInput},
		{Key: "valor", Header: "Valor", Type: googlesheet.TypeMoney, Source: googlesheet.SourceInput},
		{Key: "status", Header: "Status", Type: googlesheet.TypeChoice, Options: []string{"Novo", "Pago"}, Source: googlesheet.SourceInput},
		{Key: "aceitou", Header: "Aceitou", Type: googlesheet.TypeBoolean, Source: googlesheet.SourceInput},
	}
}

func TestCreateSpreadsheetBuildsTheHeaderFormatsAndProtection(t *testing.T) {
	client, calls := fakeGoogle(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, ":batchUpdate") {
			return 200, `{}`
		}
		return 200, `{"spreadsheetId":"sp-1","sheets":[{"properties":{"sheetId":77,"title":"Pedidos"}}]}`
	})
	created, err := client.CreateSpreadsheet(context.Background(), "token", googlesheet.SpreadsheetSpec{
		Title: "Pedidos", TabName: "Pedidos", Locale: "pt_BR", TimeZone: "America/Sao_Paulo", Columns: columns(),
	})
	if err != nil {
		t.Fatalf("CreateSpreadsheet: %v", err)
	}
	if created.SpreadsheetID != "sp-1" || created.TabID != 77 {
		t.Fatalf("created = %+v", created)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d, want create plus one batch update", len(*calls))
	}
	create := (*calls)[0]
	props := create.body["properties"].(map[string]any)
	if props["locale"] != "pt_BR" || props["timeZone"] != "America/Sao_Paulo" {
		t.Fatalf("properties = %v", props)
	}
	batch, _ := json.Marshal((*calls)[1].body)
	text := string(batch)
	for _, want := range []string{`"stringValue":"ref"`, `"stringValue":"Nome"`, `"hiddenByUser":true`, `"CURRENCY"`, `"ONE_OF_LIST"`, `"BOOLEAN"`, `"warningOnly":true`} {
		if !strings.Contains(text, want) {
			t.Fatalf("batch update misses %s: %s", want, text)
		}
	}
}

func TestAppendRowsWritesRawValuesAndReturnsTheFirstRow(t *testing.T) {
	client, calls := fakeGoogle(t, func(*http.Request) (int, string) {
		return 200, `{"updates":{"updatedRange":"'Pedidos'!A5:E7","updatedRows":3}}`
	})
	result, err := client.AppendRows(context.Background(), "token", googlesheet.Target{SpreadsheetID: "sp-1", TabName: "Pedidos"}, [][]any{{"r1", "=1+1"}})
	if err != nil {
		t.Fatalf("AppendRows: %v", err)
	}
	if result.FirstRow != 5 || result.Rows != 3 {
		t.Fatalf("result = %+v", result)
	}
	call := (*calls)[0]
	if !strings.Contains(call.query, "valueInputOption=RAW") || !strings.Contains(call.query, "insertDataOption=INSERT_ROWS") {
		t.Fatalf("query = %s; values must be written raw so text never becomes a formula", call.query)
	}
	if !strings.HasSuffix(call.path, ":append") || !strings.Contains(call.path, "%27Pedidos%27%21A1") {
		t.Fatalf("path = %s", call.path)
	}
}

func TestTabInfoFindsTheTabByID(t *testing.T) {
	client, _ := fakeGoogle(t, func(*http.Request) (int, string) {
		return 200, `{"sheets":[{"properties":{"sheetId":1,"title":"Outra"}},{"properties":{"sheetId":77,"title":"Renomeada","gridProperties":{"rowCount":1200,"columnCount":6}}}]}`
	})
	info, err := client.TabInfo(context.Background(), "token", googlesheet.Target{SpreadsheetID: "sp-1", TabID: 77})
	if err != nil || info.Title != "Renomeada" || info.Rows != 1200 || info.Columns != 6 {
		t.Fatalf("TabInfo = %+v, %v", info, err)
	}
	_, err = client.TabInfo(context.Background(), "token", googlesheet.Target{SpreadsheetID: "sp-1", TabID: 99})
	if got := (Classifier{}).Classify(err); got.Kind != googlesheet.FailureNotFound {
		t.Fatalf("a removed tab classified as %s", got.Kind)
	}
}

func TestApplyColumnsGrowsTheGridBeforeWritingNewHeaders(t *testing.T) {
	client, calls := fakeGoogle(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"sheets":[{"properties":{"sheetId":77,"title":"Pedidos","gridProperties":{"rowCount":1000,"columnCount":3}}}]}`
		}
		return 200, `{}`
	})
	if err := client.ApplyColumns(context.Background(), "token", googlesheet.Target{SpreadsheetID: "sp-1", TabID: 77, TabName: "Pedidos"}, columns()); err != nil {
		t.Fatalf("ApplyColumns: %v", err)
	}
	batch, _ := json.Marshal((*calls)[1].body)
	if !strings.Contains(string(batch), `"appendDimension"`) || !strings.Contains(string(batch), `"length":2`) {
		t.Fatalf("grid not grown: %s", batch)
	}
}

func TestClassifier(t *testing.T) {
	cases := []struct {
		status     int
		body       string
		retryAfter string
		want       googlesheet.FailureKind
	}{
		{status: 401, want: googlesheet.FailureAuth},
		{status: 429, retryAfter: "7", want: googlesheet.FailureRateLimited},
		{status: 403, body: `{"error":{"status":"PERMISSION_DENIED","message":"no access"}}`, want: googlesheet.FailurePermission},
		{status: 403, body: `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`, want: googlesheet.FailureRateLimited},
		{status: 404, want: googlesheet.FailureNotFound},
		{status: 400, body: `{"error":{"message":"Unable to parse range: 'Pedidos'!A1"}}`, want: googlesheet.FailureNotFound},
		{status: 400, body: `{"error":{"message":"Invalid value at 'data.values[1]'"}}`, want: googlesheet.FailureInvalid},
		{status: 503, want: googlesheet.FailureTransient},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.status)+c.body, func(t *testing.T) {
			res := &http.Response{StatusCode: c.status, Header: http.Header{}}
			if c.retryAfter != "" {
				res.Header.Set("Retry-After", c.retryAfter)
			}
			failure := (Classifier{}).Classify(newAPIError(res, []byte(c.body)))
			if failure.Kind != c.want {
				t.Fatalf("Classify = %s, want %s", failure.Kind, c.want)
			}
			if c.retryAfter != "" && failure.RetryAfter != 7*time.Second {
				t.Fatalf("RetryAfter = %v", failure.RetryAfter)
			}
		})
	}
	if got := (Classifier{}).Classify(&TransportError{Err: context.DeadlineExceeded}); got.Kind != googlesheet.FailureTransient {
		t.Fatalf("a timeout classified as %s", got.Kind)
	}
}

type counters struct {
	cache.SharedState
	mu     sync.Mutex
	values map[string]int64
	down   bool
}

func (c *counters) TryIncrBy(key string, delta, max int64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.down {
		return false, errors.New("redis down")
	}
	if c.values[key]+delta > max {
		return false, nil
	}
	c.values[key] += delta
	return true, nil
}

func (c *counters) DecrBy(key string, amount int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] -= amount
	return c.values[key], nil
}

func (c *counters) GetString(key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strconv.FormatInt(c.values[key], 10), nil
}

func TestLimiterHonoursBothBucketsAndGivesBackUnusedProjectSlots(t *testing.T) {
	state := &counters{values: map[string]int64{}}
	limiter, err := NewLimiter(state, Quota{ProjectWritesPerMinute: 3, ProjectReadsPerMinute: 3, UserWritesPerMinute: 2, UserReadsPerMinute: 2})
	if err != nil {
		t.Fatal(err)
	}
	limiter.now = func() time.Time { return time.Unix(6000, 0) }
	ctx := context.Background()
	for i, want := range []bool{true, true, false} {
		got, err := limiter.TakeWrite(ctx, "a@example.com")
		if err != nil || got != want {
			t.Fatalf("take %d for a = %v, %v; want %v", i, got, err, want)
		}
	}
	if got, _ := limiter.TakeWrite(ctx, "b@example.com"); !got {
		t.Fatal("the project slot refused by account a was not given back")
	}
	if got, _ := limiter.TakeWrite(ctx, "c@example.com"); got {
		t.Fatal("the project bucket was exceeded")
	}
	pressure, _ := limiter.WritePressure(ctx)
	if pressure != 1 {
		t.Fatalf("pressure = %v, want 1", pressure)
	}
}

func TestLimiterRefusesWhenRedisIsDown(t *testing.T) {
	limiter, _ := NewLimiter(&counters{values: map[string]int64{}, down: true}, Quota{ProjectWritesPerMinute: 3, ProjectReadsPerMinute: 3, UserWritesPerMinute: 2, UserReadsPerMinute: 2})
	granted, err := limiter.TakeWrite(context.Background(), "a@example.com")
	if granted || err == nil {
		t.Fatalf("TakeWrite = %v, %v; a limiter without its store must refuse", granted, err)
	}
}
