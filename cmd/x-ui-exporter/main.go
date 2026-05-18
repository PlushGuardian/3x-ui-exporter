package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"x-ui-exporter/internal/api"
	"x-ui-exporter/internal/config"
	"x-ui-exporter/internal/metrics"

	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	version = "unknown"
	commit  = "unknown"
)

func init() { //
	prometheus.MustRegister(
		// User-related metrics
		metrics.OnlineUsersCount,
		// Client-related metrics
		metrics.InboundUp,
		metrics.InboundDown,
		metrics.ClientUp,
		metrics.ClientDown,
		// System-related metrics
		metrics.XrayVersion,
		metrics.PanelThreads,
		metrics.PanelMemory,
		metrics.PanelUptime,
	)
}

func BasicAuthMiddleware(username, password string, protectedMetrics bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if protectedMetrics {
				user, pass, ok := r.BasicAuth()
				if !ok || user != username || pass != password {
					w.Header().Set("WWW-Authenticate", `Basic realm="metrics"`)
					http.Error(w, "Unauthorized.", http.StatusUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func main() {
	logFile := "/var/log/x-ui-exporter.log"

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot open log file %q: %v. Logging to stderr instead.\n", logFile, err)
		file = os.Stderr
	}
	if file != nil {
		defer func() { _ = file.Close() }()
	}

	handler := slog.NewTextHandler(file, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})

	exporterLogger := slog.New(handler).With("component", "exporter")
	threeXUILogger := slog.New(handler).With("component", "3x-ui")

	cliConfig, err := config.Parse(version, commit)
	if err != nil {
		exporterLogger.Error(err.Error())
		os.Exit(1)
	}

	fmt.Println("3X-UI Exporter (https://github.com/PlushGuardian/3x-ui-exporter/)", version)

	s := gocron.NewScheduler(time.Local)
	defer s.Stop()

	client := api.NewAPIClient(api.APIConfig{
		BaseURL:            cliConfig.BaseURL,
		ApiUsername:        cliConfig.ApiUsername,
		ApiPassword:        cliConfig.ApiPassword,
		InsecureSkipVerify: cliConfig.InsecureSkipVerify,
		ClientsBytesRows:   cliConfig.ClientsBytesRows,
	})

	_, err = s.Every(cliConfig.UpdateInterval).Seconds().Do(func() {
		token, err := client.GetAuthToken()
		if err != nil {
			threeXUILogger.Error("get auth token failed", "error", err)
			return
		}
		if err := client.FetchOnlineUsersCount(token); err != nil {
			threeXUILogger.Error("Error FetchOnlineUsersCount", "error", err)
		}

		if err := client.FetchServerStatus(token); err != nil {
			threeXUILogger.Error("Error FetchServerStatus", "error", err)
		}

		if err := client.FetchInboundsList(token); err != nil {
			threeXUILogger.Error("Error FetchInboundsList", "error", err)
		}
	})
	if err != nil {
		threeXUILogger.Error("Schedule job", "error", err)
		os.Exit(1)
	}

	s.StartAsync()

	http.Handle("/metrics", BasicAuthMiddleware(
		cliConfig.MetricsUsername,
		cliConfig.MetricsPassword,
		cliConfig.ProtectedMetrics,
	)(promhttp.Handler()))

	exporterLogger.Info("Listening %s:%s", cliConfig.Ip, cliConfig.Port)
	exporterLogger.Error(http.ListenAndServe(cliConfig.Ip+":"+cliConfig.Port, nil).Error())
	os.Exit(1)
}
