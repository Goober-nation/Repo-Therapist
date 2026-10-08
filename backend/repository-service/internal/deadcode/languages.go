package deadcode

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// language is one set of per-extension rules. All supported languages share C-style comments, so
// a single stripper with a few per-language string forms covers them.
type language struct {
	name string
	exts []string
	// def matches a function definition on one line; nameGroup is its capture group for the name.
	def       *regexp.Regexp
	nameGroup int
	isPrivate func(line string, m []int) bool
	validName func(name string) bool
	// excluded names are entry points or framework callbacks invoked without a visible call.
	excluded map[string]bool
	// annotationPrefix starts an annotation/attribute line; annotationRe captures its name.
	// Any annotation not in harmless may register the function with a framework or test harness.
	annotationPrefix string
	annotationRe     *regexp.Regexp
	harmless         map[string]bool
	// skipScope opens a block whose functions are reached through an interface (Rust traits).
	skipScope *regexp.Regexp
	isTest    func(rel string) bool

	nestedComments, backtickRaw, tripleQuote, verbatimAt, rustRaw bool
}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func hasSegment(rel string, segs ...string) bool {
	for _, p := range strings.Split(rel, "/") {
		for _, s := range segs {
			if p == s {
				return true
			}
		}
	}
	return false
}

func jvmTest(rel string) bool {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	return strings.Contains(rel, "src/test/") || strings.Contains(rel, "src/androidTest/") ||
		hasSegment(rel, "test", "tests") || strings.HasSuffix(base, "Test") ||
		strings.HasSuffix(base, "Tests") || strings.HasSuffix(base, "Spec")
}

var keywords = set("if", "for", "while", "switch", "catch", "return", "new", "synchronized", "using",
	"foreach", "lock", "fixed", "sizeof", "typeof", "nameof", "when", "throw")

func notKeyword(name string) bool { return !keywords[name] }

var languages = []*language{
	{
		name:      "go",
		exts:      []string{".go"},
		def:       regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*[\[(]`),
		nameGroup: 1,
		isPrivate: func(line string, m []int) bool {
			r, _ := utf8.DecodeRuneInString(line[m[2]:])
			return r == '_' || unicode.IsLower(r)
		},
		excluded:    set("main", "init", "_"),
		isTest:      func(rel string) bool { return strings.HasSuffix(rel, "_test.go") },
		backtickRaw: true,
	},
	{
		name: "java",
		exts: []string{".java"},
		def: regexp.MustCompile(`\bprivate\s+(?:(?:static|final|synchronized|native|strictfp)\s+)*` +
			`(?:<[^>]*>\s+)?[\w.<>\[\],?\s]+?\s+([A-Za-z_]\w*)\s*\(`),
		nameGroup: 1,
		isPrivate: func(line string, m []int) bool { return !strings.Contains(line[m[0]:m[2]], "native") },
		validName: notKeyword,
		excluded:  set("main", "readObject", "writeObject", "readResolve", "writeReplace", "readObjectNoData"),

		annotationPrefix: "@",
		annotationRe:     regexp.MustCompile(`@([\w.]+)`),
		harmless: set("SuppressWarnings", "Nullable", "NonNull", "NotNull", "Deprecated",
			"CheckReturnValue", "SafeVarargs", "Contract", "Pure"),
		isTest:      jvmTest,
		tripleQuote: true,
	},
	{
		name: "kotlin",
		exts: []string{".kt", ".kts"},
		def: regexp.MustCompile(`\bprivate\s+(?:(?:inline|suspend|tailrec|operator|infix|external)\s+)*` +
			`fun\s+(?:<[^>]*>\s*)?(?:[\w.<>?, ]+\.)?([A-Za-z_]\w*)\s*\(`),
		nameGroup: 1,
		isPrivate: func(line string, m []int) bool {
			mods := line[m[0]:m[2]]
			return !strings.Contains(mods, "operator") && !strings.Contains(mods, "external")
		},
		excluded: set("main"),

		annotationPrefix: "@",
		annotationRe:     regexp.MustCompile(`@(?:\w+:)?([\w.]+)`),
		harmless: set("Suppress", "Deprecated", "JvmStatic", "JvmName", "Throws", "OptIn",
			"Nullable", "NotNull", "VisibleForTesting"),
		isTest:         jvmTest,
		nestedComments: true,
		tripleQuote:    true,
	},
	{
		name: "csharp",
		exts: []string{".cs"},
		def: regexp.MustCompile(`\bprivate\s+(?:(?:static|async|unsafe|extern|new|readonly)\s+)*` +
			`[\w.<>\[\],?\s]+?\s+([A-Za-z_]\w*)\s*(?:<[^>()]*>)?\s*\(`),
		nameGroup: 1,
		isPrivate: func(line string, m []int) bool { return !strings.Contains(line[m[0]:m[2]], "extern") },
		validName: notKeyword,
		// Main plus Unity's message methods, which the engine calls by name.
		excluded: set("Main", "Awake", "Start", "Update", "FixedUpdate", "LateUpdate", "OnEnable",
			"OnDisable", "OnDestroy", "OnGUI", "OnValidate", "Reset", "OnApplicationQuit",
			"OnApplicationPause", "OnApplicationFocus", "OnDrawGizmos", "OnDrawGizmosSelected",
			"OnTriggerEnter", "OnTriggerExit", "OnTriggerStay", "OnCollisionEnter", "OnCollisionExit",
			"OnCollisionStay", "OnTriggerEnter2D", "OnTriggerExit2D", "OnTriggerStay2D",
			"OnCollisionEnter2D", "OnCollisionExit2D", "OnCollisionStay2D", "OnMouseDown", "OnMouseUp",
			"OnMouseEnter", "OnMouseExit", "OnBecameVisible", "OnBecameInvisible"),

		annotationPrefix: "[",
		annotationRe:     regexp.MustCompile(`[\[,]\s*([\w.]+)`),
		harmless: set("MethodImpl", "Obsolete", "SuppressMessage", "Conditional", "Pure",
			"DebuggerStepThrough", "DebuggerHidden", "NotNull", "CanBeNull", "MustUseReturnValue"),
		isTest: func(rel string) bool {
			for _, p := range strings.Split(rel, "/") {
				if strings.Contains(p, "Test") || strings.Contains(p, "test") {
					return true
				}
			}
			return false
		},
		tripleQuote: true,
		verbatimAt:  true,
	},
	{
		name: "rust",
		exts: []string{".rs"},
		def: regexp.MustCompile(`^\s*(pub(?:\s*\([^)]*\))?\s+)?(?:(?:const|async|unsafe|default)\s+)*` +
			`(extern\s+(?:"[^"]*"\s+)?)?fn\s+([A-Za-z_]\w*)`),
		nameGroup: 3,
		isPrivate: func(line string, m []int) bool { return m[2] == -1 && m[4] == -1 },
		excluded:  set("main"),

		annotationPrefix: "#[",
		annotationRe:     regexp.MustCompile(`#!?\[\s*([\w:]+)`),
		harmless: set("inline", "allow", "must_use", "doc", "deprecated", "rustfmt::skip",
			"track_caller", "cold", "warn", "deny", "expect"),
		skipScope: regexp.MustCompile(`^\s*(?:pub(?:\s*\([^)]*\))?\s+)?(?:unsafe\s+)?(?:trait\s+\w|impl\b.*\bfor\s)` +
			`|\bextern\s+"[^"]*"\s*\{`),
		isTest:         func(rel string) bool { return hasSegment(rel, "tests", "benches") },
		nestedComments: true,
		rustRaw:        true,
	},
}

func languageFor(name string) *language {
	ext := strings.ToLower(path.Ext(name))
	for _, l := range languages {
		for _, e := range l.exts {
			if e == ext {
				return l
			}
		}
	}
	return nil
}

// strip returns code with comments and string-literal contents replaced by spaces (newlines kept,
// so line numbers and quote characters stay in place), and separately the string contents.
// Identifiers inside strings still count as references (reflection, templates, interpolation),
// which errs towards keeping a function alive rather than flagging it.
func strip(src []byte, l *language) (code, strs []byte) {
	code = make([]byte, len(src))
	copy(code, src)
	strs = make([]byte, 0, 256)
	blank := func(from, to int) {
		for k := from; k < to && k < len(code); k++ {
			if code[k] != '\n' {
				code[k] = ' '
			}
		}
	}
	keep := func(from, to int) {
		if to > len(src) {
			to = len(src)
		}
		strs = append(strs, src[from:to]...)
		strs = append(strs, ' ')
		blank(from, to)
	}
	n := len(src)
	at := func(k int, s string) bool { return k+len(s) <= n && string(src[k:k+len(s)]) == s }

	for i := 0; i < n; {
		c := src[i]
		switch {
		case at(i, "//"):
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case at(i, "/*"):
			depth, j := 1, i+2
			for j < n && depth > 0 {
				switch {
				case at(j, "*/"):
					depth--
					j += 2
				case l.nestedComments && at(j, "/*"):
					depth++
					j += 2
				default:
					j++
				}
			}
			blank(i, j)
			i = j
		case l.tripleQuote && at(i, `"""`):
			j := i + 3
			for j < n && !at(j, `"""`) {
				j++
			}
			keep(i+3, j)
			i = j + 3
		case l.backtickRaw && c == '`':
			j := i + 1
			for j < n && src[j] != '`' {
				j++
			}
			keep(i+1, j)
			i = j + 1
		case l.verbatimAt && (at(i, `@"`) || at(i, `$@"`) || at(i, `@$"`)):
			j := i + strings.IndexByte(string(src[i:min(i+3, n)]), '"') + 1
			start := j
			for j < n {
				if src[j] == '"' {
					if at(j, `""`) {
						j += 2
						continue
					}
					break
				}
				j++
			}
			keep(start, j)
			i = j + 1
		case l.rustRaw && c == 'r' && (i == 0 || !isIdentByte(src[i-1])) && rawHashes(src[i+1:]) >= 0:
			h := rawHashes(src[i+1:])
			start := i + 2 + h
			end := "\"" + strings.Repeat("#", h)
			j := start
			for j < n && !at(j, end) {
				j++
			}
			keep(start, j)
			i = j + len(end)
		case c == '"':
			j := i + 1
			for j < n && src[j] != '"' && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			keep(i+1, j)
			i = j + 1
		case c == '\'':
			// Char literal ('x', '\n', '\u{1F600}'); otherwise a Rust lifetime / label — leave it.
			if i+1 < n && src[i+1] == '\\' {
				j := i + 2
				for j < n && src[j] != '\'' && src[j] != '\n' {
					j++
				}
				blank(i+1, j)
				i = j + 1
				continue
			}
			_, size := utf8.DecodeRune(src[min(i+1, n):])
			if i+1+size < n && src[i+1+size] == '\'' {
				blank(i+1, i+1+size)
				i += 2 + size
				continue
			}
			i++
		default:
			i++
		}
	}
	return code, strs
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// rawHashes returns the number of '#' before the opening quote of a Rust raw string (after the
// 'r'), or -1 if b doesn't start one.
func rawHashes(b []byte) int {
	h := 0
	for h < len(b) && b[h] == '#' {
		h++
	}
	if h < len(b) && b[h] == '"' {
		return h
	}
	return -1
}
