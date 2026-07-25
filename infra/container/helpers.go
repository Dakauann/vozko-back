package container

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("Invalid duration for %s: %v", key, err)
	}

	return duration
}

func optionalDurationEnv(key string) (time.Duration, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, false
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("Invalid duration for %s: %v", key, err)
		return 0, false
	}

	return duration, true
}

func durationFromMillisEnv(key string) (time.Duration, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, false
	}

	millis, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("Invalid millisecond value for %s: %v", key, err)
		return 0, false
	}

	return time.Duration(millis) * time.Millisecond, true
}

func canonicalZMQEndpoint(raw string, defaultPort string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "tcp://") || strings.HasPrefix(trimmed, "ipc://") || strings.HasPrefix(trimmed, "inproc://") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "ws://")
	trimmed = strings.TrimPrefix(trimmed, "wss://")
	parts := strings.SplitN(trimmed, "/", 2)
	hostPort := parts[0]
	if hostPort == "" {
		return ""
	}
	if !strings.Contains(hostPort, ":") && defaultPort != "" {
		hostPort = fmt.Sprintf("%s:%s", hostPort, defaultPort)
	}
	return "tcp://" + hostPort
}
