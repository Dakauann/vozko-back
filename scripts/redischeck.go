package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func main() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()

	pong, err := client.Ping(ctx).Result()
	fmt.Printf("PING: %v, err: %v\n", pong, err)

	err = client.Set(ctx, "test:ratelimit", "hello", 0).Err()
	fmt.Printf("SET: err: %v\n", err)

	val, err := client.Get(ctx, "test:ratelimit").Result()
	fmt.Printf("GET: %v, err: %v\n", val, err)

	res, err := client.Eval(ctx, `return {1, 0}`, []string{"test"}).Result()
	fmt.Printf("EVAL: %v, err: %v\n", res, err)

	res, err = client.Eval(ctx, `
local key = KEYS[1]
local max = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local current = redis.call("INCR", key)
if current == 1 then redis.call("EXPIRE", key, window) end
if current > max then local ttl = redis.call("TTL", key); return {0, ttl} end
return {1, 0}
`, []string{"rl:test:127.0.0.1"}, 500, 60).Result()
	fmt.Printf("EVAL rate-limit script: %v, err: %v\n", res, err)

	client.Del(ctx, "test:ratelimit", "rl:test:127.0.0.1")
	client.Close()
}
