package main

import (
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"vozko/infra/container"
)

// @title			API
// @version		1.0
// @description	API de comunicação omnichannel e call center.
// @BasePath		/
// @securityDefinitions.apikey	BearerAuth
// @in				header
// @name			Authorization
func main() {
	resetHistory := flag.Bool("reset-conversation-history", false, "erase all conversation histories before starting the server (development only)")
	flag.Parse()

	loadEnv()

	go func() { log.Println("pprof on :6060"); log.Println(http.ListenAndServe("localhost:6060", nil)) }()

	app := container.New()

	if *resetHistory {
		if os.Getenv("APP_ENV") == "production" {
			log.Println("reset-conversation-history flag is ignored in production")
		} else if err := app.ResetConversationHistory(); err != nil {
			log.Fatalf("Failed to reset conversation history: %v", err)
		}
	}

	port := getPort()

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-shutdownCh
		log.Println("shutdown signal received")
		app.Shutdown()
		os.Exit(0)
	}()

	if err := app.Start(port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func loadEnv() {
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			log.Println("godotenv: no .env file found, continuing with existing environment")
		}
	}
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}
