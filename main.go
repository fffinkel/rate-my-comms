// rate-my-comms serves one-click rating links for emails and Slack posts.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	data := flag.String("data", envOr("DATA_FILE", "votes.jsonl"), "path to the votes file")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	store, err := OpenStore(*data)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newServer(store, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "addr", *addr, "data", *data)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
