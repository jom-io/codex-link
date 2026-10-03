# Third-party notices

codex-link itself is MIT licensed. Direct runtime dependencies retain their upstream licenses:

| Dependency | License | Purpose |
| --- | --- | --- |
| github.com/redis/go-redis/v9 | BSD-2-Clause | Redis client |
| github.com/aliyun/aliyun-oss-go-sdk | Apache-2.0 | Aliyun OSS client |
| github.com/zalando/go-keyring | MIT (platform files include Apache-2.0 notices) | OS credential storage |
| golang.org/x/term | BSD-3-Clause | Hidden terminal input |
| gopkg.in/yaml.v3 | MIT / Apache-2.0 | YAML setup parsing |
| modernc.org/sqlite | BSD-3-Clause; SQLite is public domain | Pure-Go SQLite driver |

Test-only miniredis is MIT. Go module versions and transitive dependencies are recorded in go.mod/go.sum. Upstream license texts/notices must be preserved when redistributing dependency sources. Full collected upstream license/notice texts are included under `third_party/licenses/` and in the release archives. Consult upstream repositories for full terms.
