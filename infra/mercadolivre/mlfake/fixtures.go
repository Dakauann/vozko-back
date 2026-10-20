package mlfake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

func sourceDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}

func DefaultAPIFixtureDir() string {
	return filepath.Join(sourceDir(), "..", "testdata")
}

func DefaultNotificationFixtureDir() string {
	return filepath.Join(sourceDir(), "..", "..", "..", "domain", "mercadolivre", "testdata")
}

type fixtureSet struct {
	dir   string
	mu    sync.Mutex
	cache map[string][]byte
}

func newFixtureSet(dir string) *fixtureSet {
	return &fixtureSet{dir: dir, cache: map[string][]byte{}}
}

func (f *fixtureSet) raw(name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if raw, ok := f.cache[name]; ok {
		return raw, nil
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		return nil, fmt.Errorf("mlfake: fixture %s: %w", name, err)
	}
	f.cache[name] = raw
	return raw, nil
}

func (f *fixtureSet) object(name string) (map[string]any, error) {
	raw, err := f.raw(name)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeNumbers(raw, &out); err != nil {
		return nil, fmt.Errorf("mlfake: fixture %s: %w", name, err)
	}
	return out, nil
}

func (f *fixtureSet) list(name string) ([]any, error) {
	raw, err := f.raw(name)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := decodeNumbers(raw, &out); err != nil {
		return nil, fmt.Errorf("mlfake: fixture %s: %w", name, err)
	}
	return out, nil
}

func (f *fixtureSet) mustObject(name string) map[string]any {
	out, err := f.object(name)
	if err != nil {
		panic(err)
	}
	return out
}

func (f *fixtureSet) mustList(name string) []any {
	out, err := f.list(name)
	if err != nil {
		panic(err)
	}
	return out
}

func decodeNumbers(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

func firstObject(list any) map[string]any {
	items, ok := list.([]any)
	if !ok || len(items) == 0 {
		return map[string]any{}
	}
	obj, ok := items[0].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return clone(obj)
}

func child(obj map[string]any, key string) map[string]any {
	if inner, ok := obj[key].(map[string]any); ok {
		return inner
	}
	inner := map[string]any{}
	obj[key] = inner
	return inner
}

func clone(obj map[string]any) map[string]any {
	raw, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := decodeNumbers(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func num(id string) json.Number { return json.Number(id) }
