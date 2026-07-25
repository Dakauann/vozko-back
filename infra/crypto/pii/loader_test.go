package pii

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func mkEnv(kv map[string]string) ([]string, func(string) string) {
	env := make([]string, 0, len(kv))
	for k, v := range kv {
		env = append(env, k+"="+v)
	}
	get := func(k string) string { return kv[k] }
	return env, get
}

func b64of(bs []byte) string { return base64.StdEncoding.EncodeToString(bs) }

func key32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestLoadFromEnviron_Happy_DefaultsToHighestVersion(t *testing.T) {
	env, get := mkEnv(map[string]string{
		"VOZKO_PII_KEK_V1":          b64of(key32(0x01)),
		"VOZKO_PII_KEK_V2":          b64of(key32(0x02)),
		"VOZKO_PII_BLIND_INDEX_KEY": b64of(key32(0xAA)),
		"UNRELATED":                 "ignore me",
	})
	s, err := LoadFromEnviron(env, get)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveKEKVersion() != 2 {
		t.Fatalf("active=%d, want 2", s.ActiveKEKVersion())
	}
}

func TestLoadFromEnviron_ExplicitActive(t *testing.T) {
	env, get := mkEnv(map[string]string{
		"VOZKO_PII_KEK_V1":             b64of(key32(0x01)),
		"VOZKO_PII_KEK_V2":             b64of(key32(0x02)),
		"VOZKO_PII_ACTIVE_KEK_VERSION": "1",
		"VOZKO_PII_BLIND_INDEX_KEY":    b64of(key32(0xAA)),
	})
	s, err := LoadFromEnviron(env, get)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveKEKVersion() != 1 {
		t.Fatalf("active=%d", s.ActiveKEKVersion())
	}
}

func TestLoadFromEnviron_NilGetenvSafe(t *testing.T) {

	env := []string{"VOZKO_PII_KEK_V1=ignored", "VOZKO_PII_BLIND_INDEX_KEY=ignored"}
	_, err := LoadFromEnviron(env, nil)
	if !errors.Is(err, ErrNoActiveKEK) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFromEnviron_RawStdBase64Fallback(t *testing.T) {

	raw := base64.RawStdEncoding.EncodeToString(key32(0x33))
	if strings.HasSuffix(raw, "=") {
		t.Fatal("setup: raw encoding should be unpadded")
	}
	env, get := mkEnv(map[string]string{
		"VOZKO_PII_KEK_V1":          raw,
		"VOZKO_PII_BLIND_INDEX_KEY": b64of(key32(0xAA)),
	})
	if _, err := LoadFromEnviron(env, get); err != nil {
		t.Fatalf("expected fallback to succeed: %v", err)
	}
}

func TestLoadFromEnviron_Errors(t *testing.T) {
	good := b64of(key32(0x01))
	goodBlind := b64of(key32(0xAA))

	cases := []struct {
		name string
		kv   map[string]string
		want error
	}{
		{
			"no KEKs",
			map[string]string{"VOZKO_PII_BLIND_INDEX_KEY": goodBlind},
			ErrNoActiveKEK,
		},
		{
			"missing blind key",
			map[string]string{"VOZKO_PII_KEK_V1": good},
			ErrBlindIndexKey,
		},
		{
			"wrong-length KEK",
			map[string]string{
				"VOZKO_PII_KEK_V1":          b64of([]byte("only-sixteen-by!")),
				"VOZKO_PII_BLIND_INDEX_KEY": goodBlind,
			},
			nil,
		},
		{
			"invalid base64 KEK",
			map[string]string{
				"VOZKO_PII_KEK_V1":          "!!!!not-base64!!!!",
				"VOZKO_PII_BLIND_INDEX_KEY": goodBlind,
			},
			nil,
		},
		{
			"short blind key after decode",
			map[string]string{
				"VOZKO_PII_KEK_V1":          good,
				"VOZKO_PII_BLIND_INDEX_KEY": b64of([]byte("too-short")),
			},
			ErrBlindIndexKey,
		},
		{
			"invalid base64 blind key",
			map[string]string{
				"VOZKO_PII_KEK_V1":          good,
				"VOZKO_PII_BLIND_INDEX_KEY": "!!!not-base64!!!",
			},
			nil,
		},
		{
			"active version not numeric",
			map[string]string{
				"VOZKO_PII_KEK_V1":             good,
				"VOZKO_PII_ACTIVE_KEK_VERSION": "abc",
				"VOZKO_PII_BLIND_INDEX_KEY":    goodBlind,
			},
			ErrNoActiveKEK,
		},
		{
			"active version out of range",
			map[string]string{
				"VOZKO_PII_KEK_V1":             good,
				"VOZKO_PII_ACTIVE_KEK_VERSION": "999",
				"VOZKO_PII_BLIND_INDEX_KEY":    goodBlind,
			},
			ErrNoActiveKEK,
		},
		{
			"active version refers to missing KEK",
			map[string]string{
				"VOZKO_PII_KEK_V1":             good,
				"VOZKO_PII_ACTIVE_KEK_VERSION": "5",
				"VOZKO_PII_BLIND_INDEX_KEY":    goodBlind,
			},
			ErrNoActiveKEK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, get := mkEnv(tc.kv)
			_, err := LoadFromEnviron(env, get)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

func TestLoadFromEnviron_IgnoresMalformedVersionSuffixes(t *testing.T) {
	env, get := mkEnv(map[string]string{
		"VOZKO_PII_KEK_V1":          b64of(key32(0x01)),
		"VOZKO_PII_KEK_Vfoo":        b64of(key32(0x02)),
		"VOZKO_PII_KEK_V0":          b64of(key32(0x03)),
		"VOZKO_PII_KEK_V256":        b64of(key32(0x04)),
		"VOZKO_PII_KEK_V2":          "",
		"VOZKO_PII_BLIND_INDEX_KEY": b64of(key32(0xAA)),
	})
	s, err := LoadFromEnviron(env, get)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveKEKVersion() != 1 {
		t.Fatalf("active=%d, want 1", s.ActiveKEKVersion())
	}
}

func TestLoadFromEnviron_SkipsMalformedEnvirons(t *testing.T) {

	env := []string{
		"NOEQUALSIGN",
		"=leading-equal",
		"VOZKO_PII_KEK_V1=" + b64of(key32(0x01)),
		"VOZKO_PII_BLIND_INDEX_KEY=" + b64of(key32(0xAA)),
	}
	get := func(k string) string {
		switch k {
		case "VOZKO_PII_KEK_V1":
			return b64of(key32(0x01))
		case "VOZKO_PII_BLIND_INDEX_KEY":
			return b64of(key32(0xAA))
		}
		return ""
	}
	if _, err := LoadFromEnviron(env, get); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromEnv_NoEnv(t *testing.T) {

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error when no PII envs are set in the host environment")
	}
}
