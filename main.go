// Command garmin-mcp is a Model Context Protocol server for the Garmin Connect
// Personal API. It runs locally and exposes the user's own Garmin data as
// tools to MCP clients such as Claude Code, Claude Desktop and Cursor.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mijndert/garmin-mcp/internal/garmin"
	"github.com/mijndert/garmin-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Set via -ldflags "-X main.version=..." at release time.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "garmin-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("garmin-mcp", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `garmin-mcp %s - MCP server for the Garmin Connect Personal API

Usage: garmin-mcp [flags]

The API key is read from (in order): --api-key-file, the %s
environment variable, or %s.
Create one at https://connect.garmin.com (Settings > Personal API Key).

Flags:
`, version, garmin.APIKeyEnvVar, garmin.DefaultKeyFile())
		fs.PrintDefaults()
	}

	var (
		apiKeyFile   = fs.String("api-key-file", "", "file containing the personal API key (overrides the environment variable)")
		baseURL      = fs.String("base-url", envOr(garmin.BaseURLEnvVar, garmin.DefaultBaseURL), "API host; use "+garmin.ChinaBaseURL+" for Garmin China accounts (env "+garmin.BaseURLEnvVar+")")
		tokenCache   = fs.String("token-cache", envOr("GARMIN_PERSONAL_API_TOKEN_CACHE", garmin.DefaultTokenCacheFile()), "where to cache the 24-hour bearer token (env GARMIN_PERSONAL_API_TOKEN_CACHE)")
		noTokenCache = fs.Bool("no-token-cache", false, "do not persist the bearer token to disk")
		downloadDir  = fs.String("download-dir", envOr("GARMIN_MCP_DOWNLOAD_DIR", defaultDownloadDir()), "default directory for download_activity_file (env GARMIN_MCP_DOWNLOAD_DIR)")
		httpAddr     = fs.String("http", "", "serve over Streamable HTTP on this address (e.g. 127.0.0.1:8787) instead of stdio")
		logLevel     = fs.String("log-level", envOr("GARMIN_MCP_LOG_LEVEL", "warn"), "log level: debug, info, warn, error (env GARMIN_MCP_LOG_LEVEL)")
		showVersion  = fs.Bool("version", false, "print the version and exit")
	)
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println("garmin-mcp", version)
		return nil
	}

	setupLogging(*logLevel)

	apiKey, keySource, err := garmin.ResolveAPIKey("", *apiKeyFile)
	if err != nil {
		return err
	}
	slog.Info("loaded personal API key", "source", keySource)

	cache := *tokenCache
	if *noTokenCache {
		cache = ""
	}
	client := garmin.NewClient(*baseURL, apiKey, cache, &http.Client{Timeout: 90 * time.Second})
	client.UserAgent = "garmin-mcp/" + version

	newServer := func() *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{
			Name:    "garmin-mcp",
			Title:   "Garmin Connect",
			Version: version,
		}, &mcp.ServerOptions{
			Instructions: tools.Instructions,
			Logger:       slog.Default(),
		})
		tools.Register(s, &tools.Deps{
			Client: client,
			Config: tools.Config{DownloadDir: *downloadDir},
		})
		return s
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		return serveHTTP(ctx, *httpAddr, newServer)
	}
	slog.Info("serving MCP over stdio", "version", version)
	return newServer().Run(ctx, &mcp.StdioTransport{})
}

func serveHTTP(ctx context.Context, addr string, newServer func() *mcp.Server) error {
	server := newServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("serving MCP over Streamable HTTP", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func setupLogging(level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelWarn
	}
	// Logs must go to stderr: stdout carries the MCP protocol in stdio mode.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "garmin-mcp")
	}
	return filepath.Join(home, "Downloads", "garmin-mcp")
}
