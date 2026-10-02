package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/joho/godotenv"

	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database"
	advertising_repository "vozko/infra/repositories/advertising"
)

func main() {
	_ = godotenv.Load()
	svc, err := pii.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	piigorm.SetService(svc)
	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatal(err)
	}
	grant, err := advertising_repository.NewGrantRepository(db).FindByID(context.Background(), os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("grant kind=%s status=%s scopes=%v granular=%v\n", grant.TokenKind, grant.Status, grant.Scopes, grant.GranularScopes)
	mac := hmac.New(sha256.New, []byte(os.Getenv("META_ADS_APP_SECRET")))
	mac.Write([]byte(grant.AccessToken))
	proof := hex.EncodeToString(mac.Sum(nil))
	appToken := os.Getenv("META_ADS_APP_ID") + "|" + os.Getenv("META_ADS_APP_SECRET")
	for _, path := range os.Args[2:] {
		u, _ := url.Parse("https://graph.facebook.com/v26.0" + path)
		q := u.Query()
		if path == "/debug_token" {
			q.Set("input_token", grant.AccessToken)
			q.Set("access_token", appToken)
		} else {
			q.Set("access_token", grant.AccessToken)
			q.Set("appsecret_proof", proof)
		}
		u.RawQuery = q.Encode()
		resp, err := http.Get(u.String())
		if err != nil {
			fmt.Println(path, "error:", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("\n== GET %s -> %d\n%s\n", path, resp.StatusCode, body)
	}
}
