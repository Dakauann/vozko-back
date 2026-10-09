package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/infra/database"
	"vozko/infra/database/schema"
	calllist_repository "vozko/infra/repositories/calllist"
)

func main() {
	page := flag.Int("page", 100, "call lists recounted per round")
	flag.Parse()
	if *page <= 0 {
		log.Fatal("calllistrecount: -page must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatalf("calllistrecount: database: %v", err)
	}
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Error)})
	if !db.Migrator().HasColumn(&schema.CallList{}, "callback_count") {
		log.Fatal("calllistrecount: call_lists has no progress counters yet; start the server once so its migrations add them")
	}
	store := calllist_repository.NewStore(db)
	after, total := "", 0
	for {
		next, recounted, err := store.RecountProgress(ctx, after, *page)
		total += recounted
		if err != nil {
			log.Fatalf("calllistrecount: after %q: %v (%d lists corrected so far)", after, err, total)
		}
		if next == "" {
			break
		}
		after = next
	}
	log.Printf("calllistrecount: %d call lists corrected", total)
}
