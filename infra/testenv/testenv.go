package testenv

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	postgresGate       = "VOZKO_TEST_DB"
	redisGate          = "VOZKO_TEST_REDIS"
	redisPasswordKey   = "VOZKO_TEST_REDIS_PASSWORD"
	appRedisKey        = "REDIS_ADDR"
	testDatabaseSuffix = "_test"
	defaultRedisPort   = "6379"

	postgresSkip = "set VOZKO_TEST_DB=1 and point DB_HOST, DB_PORT, DB_USER, DB_PASSWORD and DB_NAME at a loopback Postgres whose database name ends in _test"
	redisSkip    = "set VOZKO_TEST_REDIS to a loopback Redis that is not REDIS_ADDR"
)

var loopbackHosts = []string{"localhost", "127.0.0.1", "::1"}

var (
	ErrNotTestDatabase    = errors.New("testenv: the database name must end in _test")
	ErrNotLoopback        = errors.New("testenv: the host must be localhost, 127.0.0.1 or ::1")
	ErrInvalidPort        = errors.New("testenv: the port must be a number between 1 and 65535")
	ErrInvalidAddress     = errors.New("testenv: the address must be host:port")
	ErrDefaultRedisPort   = errors.New("testenv: the test Redis must not listen on 6379, the default port the application Redis uses")
	ErrApplicationRedis   = errors.New("testenv: the test Redis must listen on a different port from the application Redis named by REDIS_ADDR")
	ErrUnreadableAppRedis = errors.New("testenv: REDIS_ADDR is set but is not host:port, so the test Redis cannot be told apart from it")
)

type PostgresTarget struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type RedisTarget struct {
	Addr     string
	Password string
}

func Postgres(t testing.TB) PostgresTarget {
	t.Helper()
	if os.Getenv(postgresGate) != "1" {
		t.Skip(postgresSkip)
		return PostgresTarget{}
	}
	target := PostgresTarget{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
		Name:     os.Getenv("DB_NAME"),
	}
	if err := target.Check(); err != nil {
		t.Fatalf("refusing to run against Postgres at DB_HOST=%q DB_PORT=%q DB_NAME=%q: %v", target.Host, target.Port, target.Name, err)
		return PostgresTarget{}
	}
	return target
}

func Redis(t testing.TB) RedisTarget {
	t.Helper()
	addr := os.Getenv(redisGate)
	if addr == "" {
		t.Skip(redisSkip)
		return RedisTarget{}
	}
	if err := CheckRedis(addr, os.Getenv(appRedisKey)); err != nil {
		t.Fatalf("refusing to run against Redis at VOZKO_TEST_REDIS=%q (REDIS_ADDR=%q): %v", addr, os.Getenv(appRedisKey), err)
		return RedisTarget{}
	}
	return RedisTarget{Addr: addr, Password: os.Getenv(redisPasswordKey)}
}

func (p PostgresTarget) Check() error {
	if len(p.Name) <= len(testDatabaseSuffix) || !strings.HasSuffix(p.Name, testDatabaseSuffix) {
		return ErrNotTestDatabase
	}
	if !isLoopback(p.Host) {
		return ErrNotLoopback
	}
	if !isPort(p.Port) {
		return ErrInvalidPort
	}
	return nil
}

func (p PostgresTarget) DSN() string {
	return strings.Join([]string{
		setting("host", p.Host),
		setting("port", p.Port),
		setting("user", p.User),
		setting("password", p.Password),
		setting("dbname", p.Name),
		"sslmode=disable",
	}, " ")
}

func CheckRedis(addr, appAddr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ErrInvalidAddress
	}
	if !isLoopback(host) {
		return ErrNotLoopback
	}
	if !isPort(port) {
		return ErrInvalidPort
	}
	if port == defaultRedisPort {
		return ErrDefaultRedisPort
	}
	if strings.TrimSpace(appAddr) == "" {
		return nil
	}
	_, appPort, err := net.SplitHostPort(strings.TrimSpace(appAddr))
	if err != nil {
		return ErrUnreadableAppRedis
	}
	if appPort == port {
		return ErrApplicationRedis
	}
	return nil
}

func isLoopback(host string) bool {
	for _, loopback := range loopbackHosts {
		if strings.EqualFold(host, loopback) {
			return true
		}
	}
	return false
}

func isPort(port string) bool {
	if port == "" || strings.TrimLeft(port, "0123456789") != "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

var settingEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

func setting(key, value string) string {
	return key + "='" + settingEscaper.Replace(value) + "'"
}
