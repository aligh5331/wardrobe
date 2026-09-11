// Command server runs the wardrobe backend: it validates the VLM/LLM
// env var contract at startup, then serves the local API and embedded
// frontend (07-architecture.md).
package main

import (
	"log"
	"net/http"

	"wardrobe/internal/config"
)

const addr = ":8080"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	for _, warning := range cfg.Warnings() {
		log.Printf("startup warning: %s", warning)
	}

	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, http.NewServeMux()))
}
