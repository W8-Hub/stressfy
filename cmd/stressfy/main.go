package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"stressfy/internal/api"
	"stressfy/internal/config"
	"stressfy/internal/job"
	"stressfy/internal/stress"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir %q: %v", cfg.DataDir, err)
	}

	store := job.NewStore()

	// Bind the stress runner to the store and config so scheduled jobs can fire.
	run := func(j *job.Job) {
		stress.RunJob(store, cfg, j)
	}

	server := api.NewServer(cfg, store, run)

	addr := ":" + strconv.Itoa(cfg.Port)
	log.Printf("stressfy listening on %s (data dir: %s)", addr, cfg.DataDir)

	if err := http.ListenAndServe(addr, server.Router()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
