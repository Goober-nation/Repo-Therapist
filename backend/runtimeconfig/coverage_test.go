package runtimeconfig

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// notInFile are the names services read from the environment only: secrets, the loader's own
// bootstrap variable and the old aliases llm-service still accepts.
var notInFile = map[string]bool{
	"SOURCECRAFT_PAT": true, "SERVICE_TOKEN": true, "S3_ACCESS_KEY": true, "S3_SECRET_KEY": true,
	"DB_USER": true, "DB_PASSWORD": true, "POSTGRES_DSN": true, "REDIS_PASSWORD": true,
	"YANDEX_AI_API_KEY": true, "YANDEX_FOLDER_ID": true,
	"ADMIN_PASSWORD": true, "ADMIN_YANDEX_IDS": true, "AUDIT_HASH_SECRET": true, "DEV_AUTH_BYPASS_USER_ID": true,
	"APP_CONFIG":             true,
	"LLM_STRENGTH_THRESHOLD": true, "LLM_SUMMARY_REQUEST_TOPIC": true, "LLM_SUMMARY_RESULT_TOPIC": true,
}

var (
	goEnvCall = regexp.MustCompile(`\b\w*(?:[Ee]nv|Setting)\w*\(([^)]*)\)`)
	pyEnvCall = regexp.MustCompile(`\b(?:get|get_first|_seconds_env|_env_first|env)\(([^)]*)\)`)
	envName   = regexp.MustCompile(`"([A-Z][A-Z0-9_]{2,})"`)
)

// processSections maps a service directory to the app.yaml sections its processes read.
var processSections = map[string][]string{
	"aggregator-service":          Services["aggregator"],
	"gateway-service":             Services["gateway"],
	"rating-service":              Services["rating"],
	"repository-service":          Services["repository"],
	"grade-service":               Services["grade"],
	"security-wrapper-service":    Services["security_wrapper"],
	"sourcecraft-wrapper-service": Services["sourcecraft_wrapper"],
	"llm-service":                 Services["llm"],
}

// Every non-secret name a service reads must be a key of app.yaml in a section that service reads,
// so no setting silently stays env-only.
func TestEverySettingIsInAppYAML(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "app.yaml"))
	if err != nil {
		t.Skip("no config/app.yaml")
	}
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	for dir, sections := range processSections {
		view, err := file.view(append([]string{SharedSection}, sections...))
		if err != nil {
			t.Fatal(err)
		}
		var missing []string
		seen := map[string]bool{}
		root := filepath.Join("..", dir)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == "tests" || d.Name() == "gen" || d.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			var call *regexp.Regexp
			switch {
			case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
				call = goEnvCall
			case strings.HasSuffix(path, ".py") && !strings.Contains(path, "/tests/"):
				call = pyEnvCall
			default:
				return nil
			}
			src, _ := os.ReadFile(path)
			for _, m := range call.FindAllStringSubmatch(string(src), -1) {
				for _, n := range envName.FindAllStringSubmatch(m[1], -1) {
					name := n[1]
					if notInFile[name] || seen[name] {
						continue
					}
					seen[name] = true
					if _, ok := view[name]; !ok {
						missing = append(missing, name)
					}
				}
			}
			return nil
		})
		t.Logf("%s: %d setting names read", dir, len(seen))
		if len(seen) == 0 {
			t.Errorf("%s: no settings found, the scan is broken", dir)
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s reads settings that are not in its app.yaml sections: %s", dir, strings.Join(missing, ", "))
		}
	}
}
