package pii

import (
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"vozko/infra/crypto/vault"
)

const (
	EnvKEKPrefix        = "VOZKO_PII_KEK_V"
	EnvActiveKEKVersion = "VOZKO_PII_ACTIVE_KEK_VERSION"
	EnvBlindIndexKey    = "VOZKO_PII_BLIND_INDEX_KEY"
)

func LoadFromEnv() (*Service, error) {
	return LoadFromEnviron(os.Environ(), os.Getenv)
}

func LoadFromEnviron(environ []string, getenv func(string) string) (*Service, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	keys, err := collectKEKs(environ, getenv)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no %s<n> variables set", ErrNoActiveKEK, EnvKEKPrefix)
	}

	vaults := make(map[byte]*vault.Vault, len(keys))
	for ver, raw := range keys {

		v, _ := vault.New(raw, int(ver))
		vaults[ver] = v
	}

	active, err := resolveActiveVersion(getenv, keys)
	if err != nil {
		return nil, err
	}

	blindRaw := getenv(EnvBlindIndexKey)
	if blindRaw == "" {
		return nil, fmt.Errorf("%w: %s not set", ErrBlindIndexKey, EnvBlindIndexKey)
	}
	blindKey, err := decodeKey(EnvBlindIndexKey, blindRaw, 0)
	if err != nil {
		return nil, err
	}

	return New(vaults, active, blindKey)
}

func collectKEKs(environ []string, getenv func(string) string) (map[byte][]byte, error) {
	out := make(map[byte][]byte)
	for _, kv := range environ {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		name := kv[:eq]
		if !strings.HasPrefix(name, EnvKEKPrefix) {
			continue
		}
		suffix := name[len(EnvKEKPrefix):]
		ver, perr := strconv.Atoi(suffix)
		if perr != nil || ver <= 0 || ver > 255 {
			continue
		}
		val := getenv(name)
		if val == "" {
			continue
		}
		key, err := decodeKey(name, val, 32)
		if err != nil {
			return nil, err
		}
		out[byte(ver)] = key
	}
	return out, nil
}

func resolveActiveVersion(getenv func(string) string, keys map[byte][]byte) (byte, error) {
	if raw := getenv(EnvActiveKEKVersion); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 255 {
			return 0, fmt.Errorf("%w: %s=%q is not a valid 1-255 int", ErrNoActiveKEK, EnvActiveKEKVersion, raw)
		}
		if _, ok := keys[byte(n)]; !ok {
			return 0, fmt.Errorf("%w: %s=%d but %s%d is not defined", ErrNoActiveKEK, EnvActiveKEKVersion, n, EnvKEKPrefix, n)
		}
		return byte(n), nil
	}

	versions := make([]int, 0, len(keys))
	for v := range keys {
		versions = append(versions, int(v))
	}
	sort.Ints(versions)
	return byte(versions[len(versions)-1]), nil
}

func decodeKey(name, b64 string, requireLen int) ([]byte, error) {
	trimmed := strings.TrimSpace(b64)
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		raw2, err2 := base64.RawStdEncoding.DecodeString(trimmed)
		if err2 != nil {
			return nil, fmt.Errorf("pii: %s is not valid base64: %w", name, err)
		}
		raw = raw2
	}
	if requireLen > 0 && len(raw) != requireLen {
		return nil, fmt.Errorf("pii: %s must decode to %d bytes, got %d", name, requireLen, len(raw))
	}
	if requireLen == 0 && len(raw) < MinBlindIndexKeyLen {
		return nil, fmt.Errorf("%w: %s decoded to %d bytes, need >= %d", ErrBlindIndexKey, name, len(raw), MinBlindIndexKeyLen)
	}
	return raw, nil
}
