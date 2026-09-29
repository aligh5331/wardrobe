// Command server runs the wardrobe backend: it validates the VLM/LLM
// env var contract at startup, opens the local catalog, then serves the
// read-only API and the embedded frontend over Gin on a configurable
// listen address (07-architecture.md).
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"wardrobe/frontend"
	"wardrobe/internal/api"
	"wardrobe/internal/config"
	"wardrobe/internal/logging"
	"wardrobe/internal/recommend"
	"wardrobe/internal/store"
	"wardrobe/internal/tagging"
	"wardrobe/internal/weather"

	_ "github.com/joho/godotenv/autoload" // .env autoload
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// Config errors go through slog.Default() (text, stderr) because the
	// configured logger needs a valid config to exist.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("startup: " + err.Error())
		os.Exit(1)
	}
	if err := cfg.RequireLLMURL(); err != nil {
		slog.Error("startup: " + err.Error())
		os.Exit(1)
	}

	// ponytail: not installed as slog.Default(); the request logger reaches
	// handlers through api.WithLogger. The file closer is dropped on purpose.
	logger, _, err := logging.New(cfg.LogLevel, cfg.LogFormat, "logs/app.log", os.Stderr)
	if err != nil {
		slog.Error("startup: " + err.Error())
		os.Exit(1)
	}

	for _, warning := range cfg.Warnings() {
		logger.Warn("startup warning: " + warning)
	}

	st, err := store.Open(store.DefaultDBPath)
	if err != nil {
		logger.Error("startup: open store: " + err.Error())
		os.Exit(1)
	}
	defer st.Close()

	router := api.New(st, api.DefaultPhotosDir,
		api.WithLogger(logger),
		api.WithTagging(taggingProcessor(cfg), api.DefaultStagingDir),
		// No Open-Meteo call here: the client only dials on a weather request.
		api.WithWeather(weather.New(weather.DefaultForecastBaseURL, weather.DefaultGeocodingBaseURL, weather.DefaultTimeout)),
		// No LLM call here: the picker only dials on a recommendation request.
		api.WithRecommender(&recommend.Picker{
			URL: cfg.LLMURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, Temperature: cfg.LLMTemperature,
		}))
	api.ServeFrontend(router, frontend.Dist)

	logger.Info("listening on " + *addr)
	// No "startup:" here: that prefix marks failures before the listen stage.
	if err := http.ListenAndServe(*addr, router); err != nil {
		logger.Error("server stopped: " + err.Error())
		os.Exit(1)
	}
}

// taggingProcessor builds the local VLM tagging pipeline from config exactly
// as cmd/ingest does: same client options (temperature source, optional
// serialization) and same retry-once Processor. The upload route reuses it
// with no second model or policy (05-vlm-tagging-spec.md "Interactive
// tagging (web UI)").
func taggingProcessor(cfg *config.Config) *tagging.Processor {
	opts := []tagging.Option{
		tagging.WithTemperature(func() float64 { return cfg.VLMTemperature }),
	}
	if cfg.VLMSerializeRequests {
		opts = append(opts, tagging.WithSerialization(cfg.VLMRequestDelayMS))
	}
	return tagging.NewProcessor(tagging.NewClient(cfg.VLMURL, cfg.VLMAPIKey, opts...))
}
