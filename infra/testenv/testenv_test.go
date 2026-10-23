package testenv_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"vozko/infra/testenv"
)

type stopped struct{}

type recorder struct {
	testing.TB
	skipped bool
	failed  bool
	message string
}

func (r *recorder) Helper() {}

func (r *recorder) Skip(args ...any) {
	r.skipped, r.message = true, fmt.Sprint(args...)
	panic(stopped{})
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed, r.message = true, fmt.Sprintf(format, args...)
	panic(stopped{})
}

func guarded(guard func(testing.TB)) *recorder {
	r := &recorder{}
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(stopped); !ok {
					panic(v)
				}
			}
		}()
		guard(r)
	}()
	return r
}

func unset(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func postgresEnv(t *testing.T, host, port, name string) {
	t.Helper()
	t.Setenv("VOZKO_TEST_DB", "1")
	t.Setenv("DB_HOST", host)
	t.Setenv("DB_PORT", port)
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", name)
}

func postgresGuard(target *testenv.PostgresTarget) func(testing.TB) {
	return func(tb testing.TB) { *target = testenv.Postgres(tb) }
}

func redisGuard(target *testenv.RedisTarget) func(testing.TB) {
	return func(tb testing.TB) { *target = testenv.Redis(tb) }
}

func TestPostgresSkipsWhenTheGateIsUnset(t *testing.T) {
	postgresEnv(t, "db.vozko.example", "5432", "vozko")
	unset(t, "VOZKO_TEST_DB")

	var target testenv.PostgresTarget
	r := guarded(postgresGuard(&target))

	if !r.skipped || r.failed {
		t.Fatalf("skipped = %v, failed = %v; an unset gate must skip", r.skipped, r.failed)
	}
	if target != (testenv.PostgresTarget{}) {
		t.Fatalf("a skipped guard handed out %+v", target)
	}
}

func TestPostgresSkipsWhenTheGateIsNotOne(t *testing.T) {
	postgresEnv(t, "127.0.0.1", "5433", "vozkot_test")
	t.Setenv("VOZKO_TEST_DB", "true")

	r := guarded(postgresGuard(new(testenv.PostgresTarget)))

	if !r.skipped || r.failed {
		t.Fatalf("skipped = %v, failed = %v; only VOZKO_TEST_DB=1 opens the gate", r.skipped, r.failed)
	}
}

func TestPostgresFailsOnADatabaseWhoseNameDoesNotEndInTest(t *testing.T) {
	for _, name := range []string{"vozko", "vozkot", "vozko_homolog", "vozko_test_copy", "_test", ""} {
		t.Run(name, func(t *testing.T) {
			postgresEnv(t, "127.0.0.1", "5433", name)

			var target testenv.PostgresTarget
			r := guarded(postgresGuard(&target))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; DB_NAME=%q must fail loudly", r.failed, r.skipped, name)
			}
			if !strings.Contains(r.message, testenv.ErrNotTestDatabase.Error()) {
				t.Fatalf("message = %q, want the _test rule", r.message)
			}
			if target != (testenv.PostgresTarget{}) {
				t.Fatalf("a refused guard handed out %+v", target)
			}
		})
	}
}

func TestPostgresFailsOnAHostThatIsNotLoopback(t *testing.T) {
	for _, host := range []string{"db.vozko.example", "10.0.0.5", "0.0.0.0", "", "localhost,db.vozko.example", "127.0.0.2", "host.docker.internal"} {
		t.Run(host, func(t *testing.T) {
			postgresEnv(t, host, "5433", "vozkot_test")

			r := guarded(postgresGuard(new(testenv.PostgresTarget)))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; DB_HOST=%q must fail loudly", r.failed, r.skipped, host)
			}
			if !strings.Contains(r.message, testenv.ErrNotLoopback.Error()) {
				t.Fatalf("message = %q, want the loopback rule", r.message)
			}
		})
	}
}

func TestPostgresFailsOnAPortThatIsNotANumber(t *testing.T) {
	for _, port := range []string{"", "5433 host=db.vozko.example", "5433,5432", "0", "70000"} {
		t.Run(port, func(t *testing.T) {
			postgresEnv(t, "127.0.0.1", port, "vozkot_test")

			r := guarded(postgresGuard(new(testenv.PostgresTarget)))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; DB_PORT=%q must fail loudly", r.failed, r.skipped, port)
			}
		})
	}
}

func TestPostgresPassesALoopbackTestDatabase(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "LOCALHOST", "::1"} {
		t.Run(host, func(t *testing.T) {
			postgresEnv(t, host, "5433", "vozkot_test")

			var target testenv.PostgresTarget
			r := guarded(postgresGuard(&target))

			if r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v (%s); a loopback _test database must pass", r.failed, r.skipped, r.message)
			}
			want := testenv.PostgresTarget{Host: host, Port: "5433", User: "postgres", Password: "secret", Name: "vozkot_test"}
			if target != want {
				t.Fatalf("target = %+v, want %+v", target, want)
			}
		})
	}
}

func TestDSNKeepsEveryValueInsideItsOwnSetting(t *testing.T) {
	target := testenv.PostgresTarget{
		Host: "127.0.0.1", Port: "5433", User: `post gres`,
		Password: `x' dbname='vozko' host='db.vozko.example\`, Name: "vozkot_test",
	}

	config, err := pgconn.ParseConfig(target.DSN() + " search_path=probe_schema")
	if err != nil {
		t.Fatalf("parse %q: %v", target.DSN(), err)
	}

	if config.Host != "127.0.0.1" || config.Port != 5433 || config.Database != "vozkot_test" {
		t.Fatalf("host = %q, port = %d, database = %q; a value leaked into another setting", config.Host, config.Port, config.Database)
	}
	if config.User != target.User || config.Password != target.Password {
		t.Fatalf("user = %q, password = %q; values were not kept verbatim", config.User, config.Password)
	}
	if config.RuntimeParams["search_path"] != "probe_schema" {
		t.Fatalf("search_path = %q; settings appended after the DSN must still apply", config.RuntimeParams["search_path"])
	}
}

func TestRedisSkipsWhenTheAddressIsUnset(t *testing.T) {
	unset(t, "VOZKO_TEST_REDIS")
	t.Setenv("REDIS_ADDR", "localhost:6379")

	var target testenv.RedisTarget
	r := guarded(redisGuard(&target))

	if !r.skipped || r.failed {
		t.Fatalf("skipped = %v, failed = %v; an unset VOZKO_TEST_REDIS must skip", r.skipped, r.failed)
	}
	if target != (testenv.RedisTarget{}) {
		t.Fatalf("a skipped guard handed out %+v", target)
	}
}

func TestRedisFailsWhenItIsTheApplicationRedis(t *testing.T) {
	for _, pair := range [][2]string{
		{"localhost:6380", "localhost:6380"},
		{"127.0.0.1:6380", "localhost:6380"},
		{"[::1]:6380", "127.0.0.1:6380"},
		{"127.0.0.1:6380", " localhost:6380 "},
	} {
		t.Run(pair[0]+" vs "+pair[1], func(t *testing.T) {
			t.Setenv("VOZKO_TEST_REDIS", pair[0])
			t.Setenv("REDIS_ADDR", pair[1])

			var target testenv.RedisTarget
			r := guarded(redisGuard(&target))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; the application Redis must fail loudly", r.failed, r.skipped)
			}
			if !strings.Contains(r.message, testenv.ErrApplicationRedis.Error()) {
				t.Fatalf("message = %q, want the application Redis rule", r.message)
			}
			if target != (testenv.RedisTarget{}) {
				t.Fatalf("a refused guard handed out %+v", target)
			}
		})
	}
}

func TestRedisFailsOnTheDefaultRedisPortWhateverRedisAddrSays(t *testing.T) {
	for _, app := range []string{"", "localhost:6380", "redis.vozko.example:6400"} {
		t.Run("REDIS_ADDR="+app, func(t *testing.T) {
			t.Setenv("VOZKO_TEST_REDIS", "127.0.0.1:6379")
			t.Setenv("REDIS_ADDR", app)

			var target testenv.RedisTarget
			r := guarded(redisGuard(&target))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; the default Redis port must fail loudly", r.failed, r.skipped)
			}
			if !strings.Contains(r.message, testenv.ErrDefaultRedisPort.Error()) {
				t.Fatalf("message = %q, want the default port rule", r.message)
			}
			if target != (testenv.RedisTarget{}) {
				t.Fatalf("a refused guard handed out %+v", target)
			}
		})
	}
}

func TestRedisFailsOnAnAddressThatIsNotLoopback(t *testing.T) {
	for _, addr := range []string{"redis.vozko.example:6381", "10.0.0.7:6381", "127.0.0.1", "127.0.0.1:port"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("VOZKO_TEST_REDIS", addr)
			t.Setenv("REDIS_ADDR", "localhost:6379")

			r := guarded(redisGuard(new(testenv.RedisTarget)))

			if !r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v; VOZKO_TEST_REDIS=%q must fail loudly", r.failed, r.skipped, addr)
			}
		})
	}
}

func TestRedisFailsWhenTheApplicationRedisCannotBeRead(t *testing.T) {
	t.Setenv("VOZKO_TEST_REDIS", "127.0.0.1:6381")
	t.Setenv("REDIS_ADDR", "localhost")

	r := guarded(redisGuard(new(testenv.RedisTarget)))

	if !r.failed || !errors.Is(testenv.CheckRedis("127.0.0.1:6381", "localhost"), testenv.ErrUnreadableAppRedis) {
		t.Fatalf("failed = %v (%s); an unreadable REDIS_ADDR must fail closed", r.failed, r.message)
	}
}

func TestRedisPassesALoopbackRedisThatIsNotTheApplicationOne(t *testing.T) {
	for _, app := range []string{"localhost:6379", ""} {
		t.Run("REDIS_ADDR="+app, func(t *testing.T) {
			t.Setenv("VOZKO_TEST_REDIS", "127.0.0.1:6381")
			t.Setenv("VOZKO_TEST_REDIS_PASSWORD", "lease")
			t.Setenv("REDIS_ADDR", app)

			var target testenv.RedisTarget
			r := guarded(redisGuard(&target))

			if r.failed || r.skipped {
				t.Fatalf("failed = %v, skipped = %v (%s); a separate loopback Redis must pass", r.failed, r.skipped, r.message)
			}
			if target != (testenv.RedisTarget{Addr: "127.0.0.1:6381", Password: "lease"}) {
				t.Fatalf("target = %+v", target)
			}
		})
	}
}
