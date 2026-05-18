package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"x-ui-exporter/internal/api"
	"x-ui-exporter/internal/config"
	"x-ui-exporter/internal/metrics"

	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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
	cfgFile := "/etc/x-ui-exporter/config.yaml"

	// =====| Create loggers |===================================================
	handler := getLogHandler(logFile)
	exporterLogger := slog.New(handler).With("component", "exporter")
	threeXUILogger := slog.New(handler).With("component", "3x-ui")

	// =====| Create flags |=====================================================
	v := viper.New()
	fs := pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
	fs.SortFlags = false

	err := config.InitializeFlags(v, fs)
	if errors.Is(err, config.ErrVersionRequested) {
		fmt.Printf("x-ui-exporter version %s (commit %s)\n", version, commit)
		os.Exit(0)
	}
	if err != nil {
		exporterLogger.Error("could not initialize viper flags", "error", err)
		os.Exit(1)
	}

	// =====| Read config file |=================================================
	if val := v.GetString("config-file"); val != "" {
		cfgFile = val
	}
	v.SetConfigFile(cfgFile)
	if err = v.ReadInConfig(); err != nil {
		exporterLogger.Error("failed to read config file, using default configuration.", "config file", cfgFile, "error", err)
	}

	// =====| Set environmental variables |======================================
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// =====| Unmarshal into config |============================================
	var cfg config.Config
	if err := v.Unmarshal(&cfg); err != nil {
		exporterLogger.Error("failed to unmarshal config", "config file", cfgFile, "error", err)
	}
	config.SetupConfigWatch(v, &cfg, exporterLogger)

	// =====| Build client |=====================================================
	client := api.NewAPIClient(api.APIConfig{
		BaseURL:            cfg.ThreeXUI.PanelURL(),
		ApiUsername:        cfg.ThreeXUI.Username,
		ApiPassword:        cfg.ThreeXUI.Password,
		InsecureSkipVerify: cfg.ThreeXUI.InsecureSkipVerify,
		ClientsBytesRows:   cfg.ThreeXUI.ClientsBytesRows,
	})

	// =====| Schedule scraping |=====================================================
	s := gocron.NewScheduler(time.Local)
	defer s.Stop()

	_, err = s.Every(cfg.Exporter.UpdateInterval).Seconds().Do(func() {
		if err := client.FetchMetrics(); err != nil {
			threeXUILogger.Error("could not fetch metrics", "error", err)
		}
	})
	if err != nil {
		threeXUILogger.Error("error while scheduling job", "error", err)
		os.Exit(1)
	}
	s.StartAsync()

	// =====| Add metrics handle |====================================================
	http.Handle("/metrics", BasicAuthMiddleware(
		cfg.Exporter.MetricsUsername,
		cfg.Exporter.MetricsPassword,
		cfg.Exporter.ProtectedMetrics,
	)(promhttp.Handler()))

	exporterLogger.Info("Listening %s:%s", cfg.Exporter.ListenIP, cfg.Exporter.MetricsPort)
	exporterLogger.Error(http.ListenAndServe(cfg.Exporter.ListenIP+":"+strconv.Itoa(cfg.Exporter.MetricsPort), nil).Error())
	os.Exit(1)
}
