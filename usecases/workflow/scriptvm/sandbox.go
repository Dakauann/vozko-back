package scriptvm

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/httptrace"
)

type Capabilities struct {
	Input   map[string]interface{}
	State   StateAPI
	Secrets map[string]string
	Audit   func(AuditRecord)
}

type StateAPI interface {
	Lookup(path string) (interface{}, bool)
	Set(key string, value interface{})
}

type AuditRecord struct {
	WallMs          int64
	FetchCalls      int
	SSRFBlocked     int
	SecretsAccessed []string
	StateWrites     []string
	Errored         bool
	ErrorMsg        string
	Logs            []LogEntry
	Fetches         []workflow.HTTPExchange
}

type Result struct {
	Value   interface{}
	Logs    []LogEntry
	WallMs  int64
	Audit   AuditRecord
	Returns bool
}

type LogEntry struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

type Sandbox struct {
	limits Limits
}

func New(limits Limits) *Sandbox {
	return &Sandbox{limits: limits.withDefaults()}
}

func disabledFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WORKFLOW_SCRIPT_DISABLED")))
	return v == "1" || v == "true" || v == "yes"
}

var staticBan = regexp.MustCompile(
	`\b(eval|Function)\s*\(|` +
		`\brequire\s*\(|` +
		`\bimport\s*\(|` +
		`\bWebAssembly\b|` +
		`\bXMLHttpRequest\b|` +
		`\bWorker\b|` +
		`\bSharedWorker\b|` +
		`\bnew\s+Function\b|` +
		`__proto__|prototype\s*\[\s*['"]constructor['"]\s*\]`,
)

func PreCheck(code string) error {
	if loc := staticBan.FindStringIndex(code); loc != nil {
		return fmt.Errorf("%w: %q", ErrStaticReject, code[loc[0]:loc[1]])
	}
	return nil
}

func (s *Sandbox) Run(ctx context.Context, code string, caps Capabilities) (*Result, error) {
	if disabledFromEnv() {
		return nil, ErrDisabled
	}
	if err := PreCheck(code); err != nil {
		return nil, err
	}

	start := time.Now()
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	logs := make([]LogEntry, 0, 4)
	var logBytes int
	var fetchCount int32
	var ssrfBlocked int32
	var secretAccess []string
	var stateWrites []string
	var fetches []workflow.HTTPExchange
	var stateMu sync.Mutex

	limits := s.limits
	execCtx, cancel := context.WithTimeout(ctx, limits.Wallclock)
	defer cancel()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-execCtx.Done():
			vm.Interrupt(ErrTimeout)
		case <-done:
		}
	}()

	inputSrc := caps.Input
	if inputSrc == nil {
		inputSrc = map[string]interface{}{}
	}
	inputObj := vm.ToValue(inputSrc).ToObject(vm)
	freeze(vm, inputObj)
	_ = vm.Set("input", inputObj)

	stateObj := vm.NewObject()
	_ = stateObj.Set("get", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		if caps.State == nil {
			return goja.Undefined()
		}
		if v, ok := caps.State.Lookup(key); ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	_ = stateObj.Set("set", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		val := call.Argument(1).Export()
		if caps.State != nil {
			caps.State.Set(key, val)
		}
		stateMu.Lock()
		stateWrites = append(stateWrites, key)
		stateMu.Unlock()
		return goja.Undefined()
	})
	_ = vm.Set("state", stateObj)

	_ = vm.Set("log", func(call goja.FunctionCall) goja.Value {
		level := "info"
		var msg string
		args := call.Arguments
		if len(args) >= 2 {
			level = args[0].String()
			msg = stringify(args[1:])
		} else if len(args) == 1 {
			msg = stringify(args)
		}
		if logBytes+len(msg) > limits.MaxLogBytes {
			panic(vm.NewTypeError("scriptvm: log budget exceeded"))
		}
		logBytes += len(msg)
		logs = append(logs, LogEntry{Level: level, Msg: msg})
		return goja.Undefined()
	})

	secretsObj := vm.NewObject()
	_ = secretsObj.Set("get", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		stateMu.Lock()
		secretAccess = append(secretAccess, name)
		stateMu.Unlock()
		v, ok := caps.Secrets[name]
		if !ok {
			panic(vm.NewTypeError(fmt.Sprintf("secrets.get: o segredo %q não existe neste workspace ou não foi lido com o nome literal, use secrets.get('%s') e crie-o em Segredos do workspace", name, name)))
		}
		return vm.ToValue(v)
	})
	_ = vm.Set("secrets", secretsObj)

	jsonObj := vm.NewObject()
	_ = jsonObj.Set("parse", func(call goja.FunctionCall) goja.Value {
		var v interface{}
		if err := json.Unmarshal([]byte(call.Argument(0).String()), &v); err != nil {
			panic(vm.NewTypeError(err.Error()))
		}
		return vm.ToValue(v)
	})
	_ = jsonObj.Set("stringify", func(call goja.FunctionCall) goja.Value {
		b, err := json.Marshal(call.Argument(0).Export())
		if err != nil {
			panic(vm.NewTypeError(err.Error()))
		}
		return vm.ToValue(string(b))
	})
	_ = vm.Set("json", jsonObj)

	b64Obj := vm.NewObject()
	_ = b64Obj.Set("encode", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(base64.StdEncoding.EncodeToString([]byte(call.Argument(0).String())))
	})
	_ = b64Obj.Set("decode", func(call goja.FunctionCall) goja.Value {
		b, err := base64.StdEncoding.DecodeString(call.Argument(0).String())
		if err != nil {
			panic(vm.NewTypeError(err.Error()))
		}
		return vm.ToValue(string(b))
	})
	_ = vm.Set("b64", b64Obj)

	cryptoObj := vm.NewObject()
	_ = cryptoObj.Set("uuid", func(call goja.FunctionCall) goja.Value {
		var b [16]byte
		_, _ = rand.Read(b[:])
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return vm.ToValue(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
	})
	_ = cryptoObj.Set("hash", func(call goja.FunctionCall) goja.Value {
		alg := strings.ToLower(call.Argument(0).String())
		data := []byte(call.Argument(1).String())
		var sum []byte
		switch alg {
		case "sha256":
			h := sha256.Sum256(data)
			sum = h[:]
		case "sha1":
			h := sha1.Sum(data)
			sum = h[:]
		case "md5":
			h := md5.Sum(data)
			sum = h[:]
		default:
			panic(vm.NewTypeError("crypto.hash: unsupported alg " + alg))
		}
		return vm.ToValue(hex.EncodeToString(sum))
	})
	_ = vm.Set("crypto", cryptoObj)

	_ = vm.Set("sleep", func(call goja.FunctionCall) goja.Value {
		ms := call.Argument(0).ToInteger()
		if ms < 0 {
			ms = 0
		}
		if ms > limits.Wallclock.Milliseconds() {
			ms = limits.Wallclock.Milliseconds()
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-execCtx.Done():
			panic(vm.NewTypeError("scriptvm: timeout during sleep"))
		}
		return goja.Undefined()
	})

	if limits.AllowFetch {
		client := makeSafeFetchClient(execCtx, limits.Wallclock)
		_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
			if int(atomic.LoadInt32(&fetchCount)) >= limits.MaxFetchCalls {
				panic(vm.NewTypeError("scriptvm: fetch call budget exceeded"))
			}
			atomic.AddInt32(&fetchCount, 1)

			arg := call.Argument(0)
			var opts fetchOptions
			switch a := arg.Export().(type) {
			case string:
				opts.URL = a
			case map[string]interface{}:
				b, _ := json.Marshal(a)
				_ = json.Unmarshal(b, &opts)
			default:
				panic(vm.NewTypeError("fetch: expected url string or options object"))
			}

			method := strings.ToUpper(opts.Method)
			if method == "" {
				method = http.MethodGet
			}
			exchange := workflow.HTTPExchange{Method: method, URL: opts.URL}
			record := func() {
				stateMu.Lock()
				fetches = append(fetches, exchange)
				stateMu.Unlock()
			}

			u, err := validateFetchURL(opts.URL, limits)
			if err != nil {
				atomic.AddInt32(&ssrfBlocked, 1)
				exchange.EgressRefusal = err.Error()
				record()
				panic(vm.NewTypeError(err.Error()))
			}

			var body string
			if opts.Body != nil {
				switch b := opts.Body.(type) {
				case string:
					body = b
				default:
					if encoded, err := json.Marshal(b); err == nil {
						body = string(encoded)
					}
				}
			}

			req, err := http.NewRequestWithContext(execCtx, method, u.String(), strings.NewReader(body))
			if err != nil {
				exchange.Error = err.Error()
				record()
				panic(vm.NewTypeError(err.Error()))
			}
			for k, v := range opts.Headers {
				req.Header.Set(k, v)
			}
			if body != "" && req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", "application/json")
			}
			exchange.URL = req.URL.String()
			exchange.RequestHeaders = httptrace.Headers(req.Header)
			exchange.RequestBody = body

			callStart := time.Now()
			resp, err := client.Do(req)
			exchange.LatencyMs = time.Since(callStart).Milliseconds()
			if err != nil {
				exchange.Error = err.Error()
				if errors.Is(err, errBlockedAddress) {
					atomic.AddInt32(&ssrfBlocked, 1)
					exchange.EgressRefusal = err.Error()
				}
				record()
				panic(vm.NewTypeError(err.Error()))
			}
			defer resp.Body.Close()
			exchange.URL = resp.Request.URL.String()
			exchange.Status = resp.StatusCode
			exchange.ResponseHeaders = httptrace.Headers(resp.Header)
			exchange.Redirects = httptrace.Redirects(resp)

			respBody, err := readCappedBody(resp.Body, limits.MaxFetchBodyBytes)
			if err != nil {
				exchange.Error = err.Error()
				exchange.Truncated = true
				record()
				panic(vm.NewTypeError(err.Error()))
			}
			exchange.ResponseBody = respBody
			record()
			hdrs := make(map[string]string, len(resp.Header))
			for k, v := range resp.Header {
				if len(v) > 0 {
					hdrs[k] = v[0]
				}
			}
			return vm.ToValue(fetchResponse{
				Status:     resp.StatusCode,
				OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
				Headers:    hdrs,
				Body:       respBody,
				URL:        resp.Request.URL.String(),
				DurationMs: time.Since(callStart).Milliseconds(),
			})
		})
	}

	for _, name := range []string{
		"eval", "Function", "WebAssembly", "Promise",
		"setTimeout", "setInterval", "clearTimeout", "clearInterval",
		"queueMicrotask",
	} {
		_ = vm.GlobalObject().Delete(name)
	}

	wrapped := wrapperPrefix + code + "\n})()"
	value, err := vm.RunString(wrapped)

	wall := time.Since(start).Milliseconds()
	stateMu.Lock()
	audit := AuditRecord{
		WallMs:          wall,
		FetchCalls:      int(fetchCount),
		SSRFBlocked:     int(ssrfBlocked),
		SecretsAccessed: secretAccess,
		StateWrites:     stateWrites,
		Logs:            logs,
		Fetches:         fetches,
	}
	stateMu.Unlock()

	if err != nil {
		audit.Errored = true
		audit.ErrorMsg = err.Error()
		if caps.Audit != nil {
			caps.Audit(audit)
		}

		if execCtx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, &ScriptError{Message: err.Error(), Stack: scriptStack(err)}
	}

	res := &Result{
		Logs:   logs,
		WallMs: wall,
		Audit:  audit,
	}
	if value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
		exported := value.Export()

		if buf, err := json.Marshal(exported); err == nil {
			if len(buf) > limits.MaxOutputBytes {
				audit.Errored = true
				audit.ErrorMsg = ErrOutputTooLarge.Error()
				if caps.Audit != nil {
					caps.Audit(audit)
				}
				return nil, ErrOutputTooLarge
			}
		}
		res.Value = exported
		res.Returns = true
	}
	if caps.Audit != nil {
		caps.Audit(audit)
	}
	return res, nil
}

const wrapperPrefix = "(function(){\n"

var wrapperLines = strings.Count(wrapperPrefix, "\n")

const nodeCodeFrame = "<código do nó>"

func scriptStack(err error) string {
	var exception *goja.Exception
	if !errors.As(err, &exception) {
		return ""
	}
	var b strings.Builder
	if value := exception.Value(); value != nil {
		b.WriteString(value.String())
	}
	for _, frame := range exception.Stack() {
		position := frame.Position()
		if position.Line == 0 {
			continue
		}
		name := frame.FuncName()
		if name == "" {
			name = nodeCodeFrame
		}
		fmt.Fprintf(&b, "\n\tat %s (%d:%d)", name, position.Line-wrapperLines, position.Column)
	}
	return b.String()
}

func freeze(vm *goja.Runtime, o *goja.Object) {
	if o == nil {
		return
	}

	objectCtor := vm.Get("Object").ToObject(vm)
	freezeFn, ok := goja.AssertFunction(objectCtor.Get("freeze"))
	if !ok {
		return
	}
	_, _ = freezeFn(goja.Undefined(), o)
}

func stringify(args []goja.Value) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		exp := a.Export()
		switch v := exp.(type) {
		case string:
			parts = append(parts, v)
		default:
			if b, err := json.Marshal(v); err == nil {
				parts = append(parts, string(b))
			} else {
				parts = append(parts, fmt.Sprintf("%v", v))
			}
		}
	}
	return strings.Join(parts, " ")
}
