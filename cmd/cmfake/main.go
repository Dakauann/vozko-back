package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("[cmfake] %v", err)
	}
	a, err := newApp(cfg, func(d time.Duration, run func()) { time.AfterFunc(d, run) })
	if err != nil {
		log.Fatalf("[cmfake] %v", err)
	}
	server := &http.Server{Addr: cfg.addr, Handler: a, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("[cmfake] listening on http://%s, webhooks go to %s, control page at /_fake", cfg.addr, cfg.webhookBase)
	log.Fatal(server.ListenAndServe())
}
