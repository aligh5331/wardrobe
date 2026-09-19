// Command server runs the wardrobe backend: it validates the VLM/LLM
// env var contract at startup, opens the local catalog, then serves the
// read-only API and the embedded frontend over Gin on a configurable
// listen address (07-architecture.md).
package main

import (
	"flag"
	"log"
	"net/http"
	"wardrobe/frontend"
	"wardrobe/internal/api"
	"wardrobe/internal/config"
	"wardrobe/internal/store"

	_ "github.com/joho/godotenv/autoload" // .env autoload
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	for _, warning := range cfg.Warnings() {
		log.Printf("startup warning: %s", warning)
	}

	st, err := store.Open(store.DefaultDBPath)
	if err != nil {
		log.Fatalf("startup: open store: %v", err)
	}
	defer st.Close()

	router := api.New(st, api.DefaultPhotosDir)
	api.ServeFrontend(router, frontend.Dist)

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, router))
}
