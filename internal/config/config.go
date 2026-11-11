// Package config reads server settings from environment variables and an
// optional YAML file. Environment variables win.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds every server setting. The yaml tag is the key in the file.
// The env tag is the variable name.
type Config struct {
	BaseURL       string `yaml:"base_url" env:"KOWHAI_BASE_URL"`
	Listen        string `yaml:"listen" env:"KOWHAI_LISTEN"`
	MetricsListen string `yaml:"metrics_listen" env:"KOWHAI_METRICS_LISTEN"`
	DBPath        string `yaml:"db_path" env:"KOWHAI_DB_PATH"`
	LogLevel      string `yaml:"log_level" env:"KOWHAI_LOG_LEVEL"`

	OIDCIssuer       string `yaml:"oidc_issuer" env:"KOWHAI_OIDC_ISSUER"`
	OIDCClientID     string `yaml:"oidc_client_id" env:"KOWHAI_OIDC_CLIENT_ID"`
	OIDCClientSecret string `yaml:"oidc_client_secret" env:"KOWHAI_OIDC_CLIENT_SECRET"`
	OIDCAdminGroup   string `yaml:"oidc_admin_group" env:"KOWHAI_OIDC_ADMIN_GROUP"`
	// OIDCCAFile trusts an extra CA for the provider, for dev set ups
	// with self-signed certificates.
	OIDCCAFile string `yaml:"oidc_ca_file" env:"KOWHAI_OIDC_CA_FILE"`
	// AdminEmails are always site admins. Comma separated.
	AdminEmails string `yaml:"admin_emails" env:"KOWHAI_ADMIN_EMAILS"`
	// DevOIDC runs a built-in test provider at /dev-oidc. Never in production.
	DevOIDC bool `yaml:"dev_oidc" env:"KOWHAI_DEV_OIDC"`

	S3Endpoint     string `yaml:"s3_endpoint" env:"KOWHAI_S3_ENDPOINT"`
	S3Region       string `yaml:"s3_region" env:"KOWHAI_S3_REGION"`
	S3Bucket       string `yaml:"s3_bucket" env:"KOWHAI_S3_BUCKET"`
	S3AccessKey    string `yaml:"s3_access_key" env:"KOWHAI_S3_ACCESS_KEY"`
	S3SecretKey    string `yaml:"s3_secret_key" env:"KOWHAI_S3_SECRET_KEY"`
	S3BackupBucket string `yaml:"s3_backup_bucket" env:"KOWHAI_S3_BACKUP_BUCKET"`

	MaxUploadMB    int    `yaml:"max_upload_mb" env:"KOWHAI_MAX_UPLOAD_MB"`
	TrustedProxies string `yaml:"trusted_proxies" env:"KOWHAI_TRUSTED_PROXIES"`
	BackupDir      string `yaml:"backup_dir" env:"KOWHAI_BACKUP_DIR"`
	RateLimitRPS   int    `yaml:"rate_limit_rps" env:"KOWHAI_RATE_LIMIT_RPS"`
}

func defaults() Config {
	return Config{
		BaseURL: "http://localhost:8080", Listen: ":8080", MetricsListen: ":9090", DBPath: "data/kowhai.db",
		LogLevel: "info", S3Region: "garage", MaxUploadMB: 25, RateLimitRPS: 20,
	}
}

// Load reads the file named by KOWHAI_CONFIG, if set, then the environment.
func Load() (Config, error) {
	c := defaults()
	if path := os.Getenv("KOWHAI_CONFIG"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse config: %w", err)
		}
	}
	v := reflect.ValueOf(&c).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		raw, ok := os.LookupEnv(t.Field(i).Tag.Get("env"))
		if !ok {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(raw)
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil {
				return c, fmt.Errorf("%s: %w", t.Field(i).Tag.Get("env"), err)
			}
			f.SetInt(int64(n))
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return c, fmt.Errorf("%s: %w", t.Field(i).Tag.Get("env"), err)
			}
			f.SetBool(b)
		}
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.BaseURL == "" {
		errs = append(errs, errors.New("KOWHAI_BASE_URL is required"))
	}
	if !c.DevOIDC && (c.OIDCIssuer == "" || c.OIDCClientID == "") {
		errs = append(errs, errors.New("KOWHAI_OIDC_ISSUER and KOWHAI_OIDC_CLIENT_ID are required"))
	}
	if _, err := c.Proxies(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Proxies parses KOWHAI_TRUSTED_PROXIES: comma-separated IPs or CIDRs.
func (c Config) Proxies() ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range strings.Split(c.TrustedProxies, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("KOWHAI_TRUSTED_PROXIES: %w", err)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("KOWHAI_TRUSTED_PROXIES: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

// AdminEmailList splits AdminEmails.
func (c Config) AdminEmailList() []string {
	var out []string
	for _, e := range strings.Split(c.AdminEmails, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}
