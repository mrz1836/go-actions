// Command example runs the pet-store API built with go-actions and serves the
// three self-documenting endpoints alongside it: /openapi.json, /openapi.yaml,
// and the browsable /_actions index. Set ADDR to override the listen address.
package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mrz1836/go-actions/examples/petstore"
)

// handler freezes the example registry and returns its http.Handler, mounting
// every action plus the self-documenting endpoints at the server root.
func handler() http.Handler {
	reg := petstore.Registry()
	reg.Freeze()
	return reg.Handler()
}

// run starts the HTTP server, resolving the listen address from the environment.
func run(getenv func(string) string) error {
	addr := getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("listening", "addr", addr, "docs", addr+"/_actions")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	if err := run(os.Getenv); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
