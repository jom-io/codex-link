package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jom-io/codex-link/internal/link"
	"github.com/zalando/go-keyring"
)

func TestInitializeCreatesAndPreservesDeviceIdentity(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	setup := filepath.Join(home, "setup.yaml")
	yaml := `name: test-mac
workspace: test
redis:
  address: localhost:6379
  allow_insecure: true
credentials:
  redis_password: dummy-test-password
`
	if e := os.WriteFile(setup, []byte(yaml), 0600); e != nil {
		t.Fatal(e)
	}
	if e := initialize(home, []string{"--config", setup}); e != nil {
		t.Fatal(e)
	}
	c, e := link.LoadConfig(home)
	if e != nil {
		t.Fatal(e)
	}
	s, e := link.LoadSecrets(c)
	if e != nil {
		t.Fatal(e)
	}
	if s.SigningPrivate == "" || s.ExchangePrivate == "" {
		t.Fatal("identity not automatically generated")
	}
	raw, _ := os.ReadFile(link.ConfigPath(home))
	for _, v := range []string{s.SigningPrivate, s.ExchangePrivate, s.RedisPassword} {
		if strings.Contains(string(raw), v) {
			t.Fatal("secret persisted outside Keychain")
		}
	}
	if e = initialize(home, []string{"--config", setup}); e != nil {
		t.Fatal(e)
	}
	next, _ := link.LoadConfig(home)
	ns, e := link.LoadSecrets(next)
	if e != nil || next.DeviceID != c.DeviceID || ns.SigningPrivate != s.SigningPrivate || ns.ExchangePrivate != s.ExchangePrivate {
		t.Fatal("reconfiguration replaced device identity")
	}
}
