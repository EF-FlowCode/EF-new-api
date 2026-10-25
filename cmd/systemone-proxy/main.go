// systemone-proxy is a standalone two-leg protocol adapter. It does not start or
// configure New API, open its database, or load upstream API keys from disk.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eflow.local/new-api/internal/systemoneproxy"
)

func main() {
	path := flag.String("config", "systemone-proxy.json", "path to the non-secret JSON configuration")
	flag.Parse()
	if err := run(*path); err != nil {
		log.Fatal(err)
	}
}

func run(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open proxy configuration: %w", err)
	}
	defer file.Close()
	config := systemoneproxy.Config{PublicListen: "127.0.0.1:8091", UpstreamListen: "127.0.0.1:8092"}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return errors.New("invalid proxy configuration JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("configuration must contain one JSON object")
	}
	proxy, err := systemoneproxy.New(config)
	if err != nil {
		return err
	}
	if config.PublicListen == "" || config.UpstreamListen == "" {
		return errors.New("both listener addresses are required")
	}
	public, err := net.Listen("tcp", config.PublicListen)
	if err != nil {
		return fmt.Errorf("bind public listener: %w", err)
	}
	defer public.Close()
	upstream, err := net.Listen("tcp", config.UpstreamListen)
	if err != nil {
		return fmt.Errorf("bind upstream listener: %w", err)
	}
	defer upstream.Close()
	server := func(handler http.Handler) *http.Server {
		return &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 130 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	}
	publicServer, upstreamServer := server(proxy.PublicHandler()), server(proxy.UpstreamHandler())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- publicServer.Serve(public) }()
	go func() { errorsCh <- upstreamServer.Serve(upstream) }()
	log.Printf("SystemOne adapter listening: public=%s upstream=%s", public.Addr(), upstream.Addr())
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
	}
	// Drain ingress first so its in-flight calls can still reach the second leg.
	drain, cancel := context.WithTimeout(context.Background(), 125*time.Second)
	defer cancel()
	_ = publicServer.Shutdown(drain)
	_ = upstreamServer.Shutdown(drain)
	_ = publicServer.Close()
	_ = upstreamServer.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
