package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLSetupAndSecretSeparation(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "setup.yaml")
	data := `name: mac-a
workspace: shared
redis:
  address: localhost:6379
  allow_insecure: true
oss:
  endpoint: https://oss-cn-hangzhou.aliyuncs.com
  bucket: private
credentials:
  redis_password: secret-password
  workspace_key: 0123456789abcdef0123456789abcdef0123456789abcdef
  access_key_id: test-id
  access_key_secret: test-secret
`
	if e := os.WriteFile(path, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	c, s, e := ReadSetupFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if s.RedisPassword != "secret-password" || s.AccessKeySecret != "test-secret" {
		t.Fatal("credentials not imported")
	}
	c.Defaults()
	if c.OSS.Region != "cn-hangzhou" {
		t.Fatal("region inference failed")
	}
	if e = SaveConfig(home, c); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(ConfigPath(home))
	for _, secret := range []string{s.RedisPassword, s.WorkspaceKey, s.AccessKeySecret, "credentials"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("credential leaked into persisted config")
		}
	}
	stat, _ := os.Stat(ConfigPath(home))
	if stat.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
	if e = os.WriteFile(path, []byte(data+"typo_field: true\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = ReadSetupFile(path); e == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestConfigRejectsUnsafeDefaults(t *testing.T) {
	c := testConfig("mac", NewID(), "localhost:6379")
	c.Redis.AllowInsecure = false
	if e := c.Validate(); e == nil {
		t.Fatal("plaintext Redis accepted implicitly")
	}
	c.Redis.TLS = true
	c.OSS.Endpoint = "http://example.com"
	c.OSS.Region = "region"
	if e := c.Validate(); e == nil {
		t.Fatal("plaintext OSS accepted")
	}
}
