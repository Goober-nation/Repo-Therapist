package scoring

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// trivialCommands do no build/test work: a pipeline made only of these always "passes" and says
// nothing about the code (docs/grading/ci-cd.md, anti-gaming).
var trivialCommands = map[string]bool{
	"echo": true, "printf": true, "true": true, ":": true, "exit": true, "sleep": true,
	"ls": true, "pwd": true, "cat": true, "date": true, "whoami": true, "env": true,
	"printenv": true, "uname": true, "hostname": true,
}

// IsNoOpCIConfig reports whether a .sourcecraft/ci.yaml does no real work. The file is laid out
// workflows → tasks → cubes; a cube either runs `script` lines or a prebuilt image. Returns true
// only when at least one cube exists and every cube's script is made of trivial commands. Any
// doubt — unparseable YAML, a task without cubes, a cube without a script (image-only, which we
// can't inspect), a non-trivial command — gives false: a real pipeline must never be zeroed
// because the check couldn't read it.
func IsNoOpCIConfig(data []byte) bool {
	var doc struct {
		Workflows map[string]struct {
			Tasks []struct {
				Cubes []struct {
					Script any `yaml:"script"`
				} `yaml:"cubes"`
			} `yaml:"tasks"`
		} `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	cubes := 0
	for _, wf := range doc.Workflows {
		for _, task := range wf.Tasks {
			if len(task.Cubes) == 0 {
				return false
			}
			for _, cube := range task.Cubes {
				lines, ok := scriptLines(cube.Script)
				if !ok {
					return false
				}
				for _, line := range lines {
					if !isTrivialLine(line) {
						return false
					}
				}
				cubes++
			}
		}
	}
	return cubes > 0
}

// scriptLines accepts `script` as a list of strings or one (possibly multi-line) string. ok is
// false for a missing or otherwise-shaped script.
func scriptLines(script any) (lines []string, ok bool) {
	switch s := script.(type) {
	case string:
		return strings.Split(s, "\n"), true
	case []any:
		for _, item := range s {
			str, isStr := item.(string)
			if !isStr {
				return nil, false
			}
			lines = append(lines, strings.Split(str, "\n")...)
		}
		return lines, true
	default:
		return nil, false
	}
}

// isTrivialLine: blank and comment lines are trivial; otherwise every command chained with
// ;, && or || must start with a trivial command (`exit` only with no status or status 0).
func isTrivialLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return true
	}
	for _, sep := range []string{"&&", "||"} {
		line = strings.ReplaceAll(line, sep, ";")
	}
	for _, cmd := range strings.Split(line, ";") {
		fields := strings.Fields(cmd)
		if len(fields) == 0 {
			continue
		}
		if !trivialCommands[fields[0]] {
			return false
		}
		if fields[0] == "exit" && len(fields) > 1 && fields[1] != "0" {
			return false
		}
	}
	return true
}
