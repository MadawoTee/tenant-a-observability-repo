// tenant-alert-router
//
// Receives Alertmanager webhook payloads, formats them, and forwards them to
// Telegram. Designed to run as a small per-tenant sidecar inside the tenant's
// monitoring namespace. The platform's Alertmanager is configured (via an
// AlertmanagerConfig CR shipped by the tenant Helm chart) to route alerts to
// this service.
//
// Stub paths for Power Automate and Microsoft Teams are wired up but disabled
// by default — flip the feature flags in values.yaml when ready.
package main
 
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)
 
// Alertmanager webhook payload — only the fields we need.
// Full schema: https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type alertPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`            // "firing" | "resolved"
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []struct {
		Status       string            `json:"status"`
		Labels       map[string]string `json:"labels"`
		Annotations  map[string]string `json:"annotations"`
		StartsAt     time.Time         `json:"startsAt"`
		EndsAt       time.Time         `json:"endsAt"`
		GeneratorURL string            `json:"generatorURL"`
	} `json:"alerts"`
}
 
type config struct {
	listenAddr           string
	telegramBotToken     string
	telegramChatID       string
	tenant               string
	featurePowerAutomate bool
	featureTeams         bool
}
 
func loadConfig() config {
	return config{
		listenAddr:           getenv("LISTEN_ADDR", ":8080"),
		telegramBotToken:     os.Getenv("TELEGRAM_BOT_TOKEN"),
		telegramChatID:       os.Getenv("TELEGRAM_CHAT_ID"),
		tenant:               getenv("TENANT", "unknown"),
		featurePowerAutomate: getenv("FEATURE_POWER_AUTOMATE", "false") == "true",
		featureTeams:         getenv("FEATURE_TEAMS", "false") == "true",
	}
}
 
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
 
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
 
	cfg := loadConfig()
	if cfg.telegramBotToken == "" || cfg.telegramChatID == "" {
		slog.Warn("telegram credentials missing — webhooks will be logged but not delivered")
	}
 
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/webhook", webhookHandler(cfg))
 
	srv := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
 
	go func() {
		slog.Info("listening", "addr", cfg.listenAddr, "tenant", cfg.tenant)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()
 
	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
 
func webhookHandler(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
 
		var p alertPayload
		if err := json.Unmarshal(body, &p); err != nil {
			slog.Error("decode payload", "err", err, "body", string(body))
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
 
		msg := formatTelegram(cfg.tenant, p)
		slog.Info("alert received",
			"status", p.Status,
			"receiver", p.Receiver,
			"alerts", len(p.Alerts),
		)
 
		if cfg.telegramBotToken != "" && cfg.telegramChatID != "" {
			if err := sendTelegram(r.Context(), cfg, msg); err != nil {
				slog.Error("telegram send failed", "err", err)
				// Don't 5xx — that would make Alertmanager retry indefinitely.
				// Just log and ack.
			}
		}
 
		if cfg.featurePowerAutomate {
			slog.Info("power automate path not implemented yet")
		}
		if cfg.featureTeams {
			slog.Info("teams path not implemented yet")
		}
 
		w.WriteHeader(http.StatusOK)
	}
}
 
// formatTelegram builds a compact HTML message for the Telegram Bot API.
// Telegram supports a small HTML subset: https://core.telegram.org/bots/api#html-style
func formatTelegram(tenant string, p alertPayload) string {
	var emoji string
	switch p.Status {
	case "firing":
		emoji = "\U0001F525" // fire
	case "resolved":
		emoji = "✅" // check mark
	default:
		emoji = "ℹ️" // info
	}
 
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>[%s] %s</b>\n", emoji, strings.ToUpper(p.Status), htmlEscape(tenant))
	if name := p.CommonLabels["alertname"]; name != "" {
		fmt.Fprintf(&b, "<b>Alert:</b> %s\n", htmlEscape(name))
	}
	if sev := p.CommonLabels["severity"]; sev != "" {
		fmt.Fprintf(&b, "<b>Severity:</b> %s\n", htmlEscape(sev))
	}
	if ns := p.CommonLabels["namespace"]; ns != "" {
		fmt.Fprintf(&b, "<b>Namespace:</b> %s\n", htmlEscape(ns))
	}
	if summary := p.CommonAnnotations["summary"]; summary != "" {
		fmt.Fprintf(&b, "\n%s\n", htmlEscape(summary))
	}
	if desc := p.CommonAnnotations["description"]; desc != "" {
		fmt.Fprintf(&b, "\n<i>%s</i>\n", htmlEscape(desc))
	}
	if len(p.Alerts) > 1 {
		fmt.Fprintf(&b, "\n<b>Total alerts in group:</b> %d", len(p.Alerts))
	}
	if p.ExternalURL != "" {
		fmt.Fprintf(&b, "\n\n<a href=\"%s\">Open Alertmanager</a>", htmlEscape(p.ExternalURL))
	}
	return b.String()
}
 
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
 
func sendTelegram(ctx context.Context, cfg config, message string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.telegramBotToken)
	payload := map[string]any{
		"chat_id":                  cfg.telegramChatID,
		"text":                     message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, _ := json.Marshal(payload)
 
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
 
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("telegram api %d: %s", resp.StatusCode, string(b))
	}
	return nil
}