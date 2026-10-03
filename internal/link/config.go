package link

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

type Config struct {
	PairingHeadless bool   `json:"pairing_headless,omitempty" yaml:"pairing_headless,omitempty"`
	DeviceID        string `json:"device_id" yaml:"device_id"`
	Name            string `json:"name" yaml:"name"`
	Workspace       string `json:"workspace" yaml:"workspace"`
	Redis           struct {
		Address       string `json:"address" yaml:"address"`
		Username      string `json:"username,omitempty" yaml:"username,omitempty"`
		DB            int    `json:"db" yaml:"db"`
		TLS           bool   `json:"tls" yaml:"tls"`
		AllowInsecure bool   `json:"allow_insecure,omitempty" yaml:"allow_insecure,omitempty"`
	} `json:"redis" yaml:"redis"`
	OSS struct {
		Endpoint string `json:"endpoint" yaml:"endpoint"`
		Region   string `json:"region,omitempty" yaml:"region,omitempty"`
		Bucket   string `json:"bucket" yaml:"bucket"`
		Prefix   string `json:"prefix" yaml:"prefix"`
	} `json:"oss" yaml:"oss"`
	MaxFileBytes   int64 `json:"max_file_bytes" yaml:"max_file_bytes"`
	StreamMaxLen   int64 `json:"stream_max_len" yaml:"stream_max_len"`
	StreamTTLHours int   `json:"stream_ttl_hours" yaml:"stream_ttl_hours"`
}
type Secrets struct {
	RedisPassword   string `json:"redis_password" yaml:"redis_password"`
	SigningPrivate  string `json:"signing_private" yaml:"-"`
	ExchangePrivate string `json:"exchange_private" yaml:"-"`
	AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret" yaml:"access_key_secret"`
	SecurityToken   string `json:"security_token,omitempty" yaml:"security_token,omitempty"`
}

func DefaultHome() string {
	if s := os.Getenv("CODEX_LINK_HOME"); s != "" {
		return s
	}
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Application Support", "codex-link")
	}
	return filepath.Join(h, ".local", "share", "codex-link")
}
func ConfigPath(home string) string { return filepath.Join(home, "config.json") }
func LoadConfig(home string) (Config, error) {
	var c Config
	b, e := os.ReadFile(ConfigPath(home))
	if e != nil {
		return c, fmt.Errorf("run codex-link init first: %w", e)
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	c.Defaults()
	return c, c.Validate()
}
func (c *Config) Defaults() {
	if c.DeviceID == "" {
		c.DeviceID = NewID()
	}
	if c.MaxFileBytes == 0 {
		c.MaxFileBytes = 1024 * 1024 * 1024
	}
	if c.StreamMaxLen == 0 {
		c.StreamMaxLen = 10000
	}
	if c.StreamTTLHours == 0 {
		c.StreamTTLHours = 720
	}
	if c.OSS.Region == "" && c.OSS.Endpoint != "" {
		u, e := url.Parse(c.OSS.Endpoint)
		if e == nil && strings.HasPrefix(u.Hostname(), "oss-") && strings.HasSuffix(u.Hostname(), ".aliyuncs.com") {
			c.OSS.Region = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(u.Hostname(), "oss-"), ".aliyuncs.com"), "-internal")
		}
	}
	if c.OSS.Prefix == "" {
		c.OSS.Prefix = "codex-link"
	}
}
func (c Config) Validate() error {
	if !ValidLabel(c.DeviceID) || !ValidLabel(c.Name) || !ValidLabel(c.Workspace) {
		return errors.New("device ID, name and workspace must contain 1–64 ASCII letters, digits, '.', '_' or '-'")
	}
	if c.Redis.Address == "" {
		return errors.New("Redis address is required")
	}
	if !c.Redis.TLS && !c.Redis.AllowInsecure {
		return errors.New("Redis TLS is required; allow_insecure is only for trusted local testing")
	}
	if c.Redis.DB < 0 || c.MaxFileBytes <= 0 || c.StreamMaxLen < 1 || c.StreamTTLHours < 1 {
		return errors.New("invalid limits")
	}
	if c.OSS.Endpoint != "" && c.OSS.Region == "" {
		return errors.New("OSS region is required for a custom endpoint")
	}
	if c.OSS.Endpoint != "" && !strings.HasPrefix(c.OSS.Endpoint, "https://") {
		return errors.New("OSS endpoint must use HTTPS")
	}
	if strings.Contains(c.OSS.Prefix, "..") || strings.HasPrefix(c.OSS.Prefix, "/") {
		return errors.New("invalid OSS prefix")
	}
	return nil
}
func SaveConfig(home string, c Config) error {
	if e := c.Validate(); e != nil {
		return e
	}
	if e := os.MkdirAll(home, 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	tmp := ConfigPath(home) + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, ConfigPath(home))
}
func SaveSecrets(c Config, s Secrets) error {
	identity, err := EnsureIdentity(&s)
	if err != nil {
		return err
	}
	if identity.ID() != c.DeviceID {
		return errors.New("device ID does not match its identity")
	}
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return keyring.Set("io.jom.codex-link", c.DeviceID, string(b))
}
func LoadSecrets(c Config) (Secrets, error) {
	var s Secrets
	b, e := keyring.Get("io.jom.codex-link", c.DeviceID)
	if e != nil {
		return s, errors.New("cannot read Keychain credentials; run init/configure in an interactive login session")
	}
	e = json.Unmarshal([]byte(b), &s)
	if e != nil {
		return s, e
	}
	identity, err := EnsureIdentity(&s)
	if err != nil {
		return s, err
	}
	if identity.ID() != c.DeviceID {
		return s, errors.New("device identity mismatch")
	}
	return s, nil
}

// ReadSetupFile imports YAML credentials only into memory; SaveConfig never persists them.
func ReadSetupFile(path string) (Config, Secrets, error) {
	var c Config
	var s Secrets
	b, e := os.ReadFile(path)
	if e != nil {
		return c, s, e
	}
	var setup struct {
		Config      `yaml:",inline"`
		Credentials Secrets `yaml:"credentials"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if e = decoder.Decode(&setup); e != nil {
		return c, s, fmt.Errorf("invalid setup YAML/JSON: %w", e)
	}
	return setup.Config, setup.Credentials, nil
}
