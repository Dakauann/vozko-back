package whisper

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vozko/domain/stt"
)

const (
	EnvWhisperURLs          = "WHISPER_URLS"
	EnvWhisperModel         = "WHISPER_MODEL"
	EnvWhisperInitialPrompt = "WHISPER_INITIAL_PROMPT"
)

func GetURLsFromEnv(envVar string) []string {
	val := strings.TrimSpace(os.Getenv(envVar))
	if val == "" {
		return nil
	}
	urls := strings.Split(val, ",")
	result := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" {
			result = append(result, u)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

type ServerHealth struct {
	URL       string        `json:"url"`
	Healthy   bool          `json:"healthy"`
	Latency   time.Duration `json:"latency_ms"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

type PoolHealth struct {
	TotalServers   int            `json:"total_servers"`
	HealthyServers int            `json:"healthy_servers"`
	Servers        []ServerHealth `json:"servers"`
}

type PoolConfig struct {
	BaseURLs      []string
	Model         string
	ServerType    ServerType
	Language      string
	Timeout       time.Duration
	FilterConfig  *stt.FilterConfig
	Logger        *log.Logger
	InitialPrompt string
}

type Pool struct {
	clients  []*Client
	healthy  []bool
	counter  uint64
	mu       sync.RWMutex
	logger   *log.Logger
	baseURLs []string
}

func NewPool(cfg PoolConfig) (*Pool, error) {
	if len(cfg.BaseURLs) == 0 {
		return nil, errors.New("whisper pool: at least one base URL is required")
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.ServerType == "" {
		cfg.ServerType = ServerTypeWhisperCpp
	}

	clients := make([]*Client, len(cfg.BaseURLs))
	healthy := make([]bool, len(cfg.BaseURLs))
	for i, baseURL := range cfg.BaseURLs {
		clients[i] = NewClient(Config{
			BaseURL:       baseURL,
			Model:         cfg.Model,
			ServerType:    cfg.ServerType,
			Language:      cfg.Language,
			Timeout:       cfg.Timeout,
			FilterConfig:  cfg.FilterConfig,
			InitialPrompt: cfg.InitialPrompt,
		})
		healthy[i] = true
	}

	pool := &Pool{
		clients:  clients,
		healthy:  healthy,
		logger:   cfg.Logger,
		baseURLs: cfg.BaseURLs,
	}

	if cfg.Logger != nil {
		cfg.Logger.Printf("whisper pool: initialized with %d server(s): %v", len(cfg.BaseURLs), cfg.BaseURLs)
	}

	return pool, nil
}

func (p *Pool) getNextClient() (*Client, int) {
	if len(p.clients) == 1 {
		return p.clients[0], 0
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	startIdx := atomic.AddUint64(&p.counter, 1) - 1
	for i := 0; i < len(p.clients); i++ {
		idx := int((startIdx + uint64(i)) % uint64(len(p.clients)))
		if p.healthy[idx] {
			return p.clients[idx], idx
		}
	}

	idx := int(startIdx % uint64(len(p.clients)))
	return p.clients[idx], idx
}

func (p *Pool) Transcribe(ctx context.Context, audioData []byte, language string) (*stt.Transcription, error) {
	client, idx := p.getNextClient()

	if p.logger != nil && len(p.clients) > 1 {
		p.logger.Printf("whisper pool: routing transcription to server %d (audio size: %d bytes)", idx, len(audioData))
	}

	result, err := client.Transcribe(ctx, audioData, language)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("whisper pool: server %d failed: %v", idx, err)
		}

		p.mu.Lock()
		p.healthy[idx] = false
		p.mu.Unlock()
		return result, err
	}

	return result, nil
}

func (p *Pool) CheckHealth(ctx context.Context) PoolHealth {
	health := PoolHealth{
		TotalServers: len(p.clients),
		Servers:      make([]ServerHealth, len(p.clients)),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, client := range p.clients {
		wg.Add(1)
		go func(idx int, c *Client) {
			defer wg.Done()

			sh := ServerHealth{
				URL:       p.baseURLs[idx],
				CheckedAt: time.Now(),
			}

			start := time.Now()

			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, c.baseURL, nil)
			if err != nil {
				sh.Error = fmt.Sprintf("create request: %v", err)
				sh.Healthy = false
			} else {
				resp, err := http.DefaultClient.Do(req)
				sh.Latency = time.Since(start)
				if err != nil {
					sh.Error = fmt.Sprintf("connection failed: %v", err)
					sh.Healthy = false
				} else {
					resp.Body.Close()
					sh.Healthy = true
				}
			}

			mu.Lock()
			health.Servers[idx] = sh
			if sh.Healthy {
				health.HealthyServers++
			}

			p.mu.Lock()
			p.healthy[idx] = sh.Healthy
			p.mu.Unlock()
			mu.Unlock()
		}(i, client)
	}

	wg.Wait()
	return health
}

func (p *Pool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, h := range p.healthy {
		if h {
			count++
		}
	}
	return count
}

func (p *Pool) ServerCount() int {
	return len(p.clients)
}

func (p *Pool) BaseURLs() []string {
	urls := make([]string, len(p.clients))
	for i, c := range p.clients {
		urls[i] = c.baseURL
	}
	return urls
}

var _ stt.STTService = (*Pool)(nil)
