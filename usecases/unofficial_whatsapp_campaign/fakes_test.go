package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

type fakeShared struct {
	mu       sync.Mutex
	values   map[string]string
	failIncr bool
}

func newFakeShared() *fakeShared {
	return &fakeShared{values: map[string]string{}}
}

func (f *fakeShared) SetNX(key, value string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = value
	return true, nil
}

func (f *fakeShared) SetString(key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = value
	return nil
}

func (f *fakeShared) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values[key], nil
}

func (f *fakeShared) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.values, k)
	}
	return nil
}

func (f *fakeShared) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.values[key]
	return ok, nil
}

func (f *fakeShared) Incr(key string) (int64, error) { return f.IncrWithTTL(key, 0) }

func (f *fakeShared) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	if f.failIncr {
		return 0, errors.New("cache unavailable")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _ := strconv.ParseInt(f.values[key], 10, 64)
	n++
	f.values[key] = strconv.FormatInt(n, 10)
	return n, nil
}

func (f *fakeShared) Decr(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _ := strconv.ParseInt(f.values[key], 10, 64)
	n--
	f.values[key] = strconv.FormatInt(n, 10)
	return n, nil
}

func (f *fakeShared) TryIncr(key string, max int64) (bool, error) {
	n, err := f.IncrWithTTL(key, 0)
	if err != nil {
		return false, err
	}
	return n <= max, nil
}

func (f *fakeShared) SAdd(key string, members ...string) error                            { return nil }
func (f *fakeShared) SRem(key string, members ...string) error                            { return nil }
func (f *fakeShared) SMembers(key string) ([]string, error)                               { return nil, nil }
func (f *fakeShared) Publish(channel string, data []byte) error                           { return nil }
func (f *fakeShared) Subscribe(ctx context.Context, channel string, handler func([]byte)) {}
func (f *fakeShared) HSet(key, field, value string) error                                 { return nil }

func (f *fakeShared) HDel(key, field string) error { return nil }

func (f *fakeShared) HGetAll(key string) (map[string]string, error) { return map[string]string{}, nil }

func (f *fakeShared) HIncrBy(key, field string, incr int64) (int64, error) { return 0, nil }

func (f *fakeShared) IncrBy(key string, amount int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _ := strconv.ParseInt(f.values[key], 10, 64)
	n += amount
	f.values[key] = strconv.FormatInt(n, 10)
	return n, nil
}

func (f *fakeShared) DecrBy(key string, amount int64) (int64, error) {
	return f.IncrBy(key, -amount)
}

func (f *fakeShared) TryIncrBy(key string, delta, max int64) (bool, error) {
	n, err := f.IncrBy(key, delta)
	if err != nil {
		return false, err
	}
	return n <= max, nil
}

func (f *fakeShared) Expire(key string, ttl time.Duration) (bool, error) { return true, nil }
