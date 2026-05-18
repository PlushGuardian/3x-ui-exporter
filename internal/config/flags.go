package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var ErrVersionRequested = errors.New("version requested")

func InitializeFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	fs.String("config-file", "", "Path to YAML configuration file")

	fs.String("metrics-listen-ip", "0.0.0.0", "IP to listen on")
	fs.Int("metrics-port", 9100, "Port for the 3x-ui-exporter to listen on")
	fs.String("metrics-path", "/metrics", "Path the 3x-ui-exporter listens on")
	fs.Bool("metrics-protected", false, "Protect metrics with basic auth")
	fs.String("metrics-username", "metricsUser", "Metrics basic auth username")
	fs.String("metrics-password", "MetricsVeryHardPassword", "Metrics basic auth password")
	fs.Int("update-interval", 30, "Polling interval in seconds")
	fs.Int("scrape-timeout", 30, "Scrape timeout for the metrics port of the 3x-ui-exporter")
	fs.String("timezone", "UTC", "Application timezone")
	versionRequested := fs.Bool("version", false, "Print version and exit")

	fs.Int("threexui-panel-port", 2053, "3X‑UI panel port")
	fs.String("threexui-panel-path", "", "3X‑UI panel path")
	fs.String("threexui-username", "", "3X‑UI username")
	fs.String("threexui-password", "", "3X‑UI password")
	fs.Bool("threexui-insecure-skip-verify", false, "Skip TLS verification")
	fs.Int("threexui-clients-bytes-rows", 0, "Top N rows for client bytes")
	fs.Int("threexui-timeout", 15, "Request timeout for the 3x-ui panel")

	if *versionRequested {
		return ErrVersionRequested
	}

	var bindErr error
	fs.VisitAll(func(f *pflag.Flag) {
		var configKey string
		switch {
		case f.Name == "config-file":
			configKey = f.Name
		case strings.HasPrefix(f.Name, "threexui-"):
			configKey = "threexui." + strings.TrimPrefix(f.Name, "threexui-")
		default:
			configKey = "x-ui-exporter." + f.Name
		}

		if err := v.BindPFlag(configKey, f); err != nil {
			bindErr = fmt.Errorf("failed to bind flag %s to key %s: %w", f.Name, configKey, err)
		}
	})

	if bindErr != nil {
		return bindErr
	}

	return nil
}
