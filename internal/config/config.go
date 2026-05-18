package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type Config struct {
	Exporter ExporterConfig `mapstructure:"x-ui-exporter"`
	ThreeXUI ThreeXUIConfig `mapstructure:"threexui"`
}

type ExporterConfig struct {
	ListenIP         string        `mapstructure:"metrics-listen-ip"`
	MetricsPort      int           `mapstructure:"metrics-port"`
	MetricsPath      string        `mapstructure:"metrics-path"`
	ProtectedMetrics bool          `mapstructure:"metrics-protected"`
	MetricsUsername  string        `mapstructure:"metrics-username"`
	MetricsPassword  string        `mapstructure:"metrics-password"`
	UpdateInterval   int           `mapstructure:"update-interval"`
	ScrapeTimeout    time.Duration `mapstructure:"scrape-timeout"`
	Timezone         string        `mapstructure:"timezone"`
}

func (c *ExporterConfig) Addr() (string, error) {
	host := "localhost" // no other hosts used by design

	return fmt.Sprint(net.JoinHostPort(host, fmt.Sprint(c.MetricsPort))), nil
}

type ThreeXUIConfig struct {
	PanelPort          int           `mapstructure:"panel-port"`
	PanelPath          string        `mapstructure:"panel-path"`
	Username           string        `mapstructure:"username"`
	Password           string        `mapstructure:"password"`
	InsecureSkipVerify bool          `mapstructure:"insecure-skip-verify"`
	ClientsBytesRows   int           `mapstructure:"clients-bytes-rows"`
	Timeout            time.Duration `mapstructure:"timeout"`
}

func (c *ThreeXUIConfig) PanelURL() string {
	host := "localhost" // no other hosts used by design
	u := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, fmt.Sprint(c.PanelPort)),
	}
	return u.JoinPath(c.PanelPath).String()
}

type MTProxyMaxConfig struct {
	MetricsPort int    `mapstructure:"metrics-port"`
	MetricsPath string `mapstructure:"metrics-path"`
}

func (c *MTProxyMaxConfig) MetricsURL() string {
	host := "localhost" // no other hosts used by design
	u := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, fmt.Sprint(c.MetricsPort)),
	}
	return u.JoinPath(c.MetricsPath).String()
}

func SetupConfigWatch(v *viper.Viper, cfg *Config, logger *slog.Logger) {
	logger.Info("Started watching config file", "config file", v.ConfigFileUsed())
	v.OnConfigChange(func(e fsnotify.Event) {
		logger.Info("Config file changed", "config file", e.Name)
		if err := v.Unmarshal(cfg); err != nil {
			logger.Error("failed to unmarshal config", "config file", e.Name, "error", err)
			os.Exit(0)
		}
	})

	v.WatchConfig()
}
