// Package deadcode finds private functions that are declared but never referenced anywhere in the
// repo. It is deliberately syntax-only (per-language regexes plus a comment/string stripper, no
// compiler or parser), so it runs on any checkout in one pass and switches rules per file by
// extension. Only private functions are considered: a public one may be called from outside the
// repo. See docs/grading/code-health.md for the grading rationale and known limitations.
package deadcode

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxListed caps the Unused list in the export; UnusedTotal always has the full count.
const MaxListed = 1000

// maxFileBytes skips files too large to be hand-written source (bundles, generated tables).
const maxFileBytes = 2 << 20

var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, "target": true, "build": true,
	"bin": true, "obj": true, "dist": true, "out": true, ".gradle": true, ".idea": true,
}

// Function is one detected unused private function.
type Function struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Name string `json:"name"`
	Lang string `json:"lang"`
}

// LangStats is the per-language breakdown.
type LangStats struct {
	Files   int `json:"files"`
	Private int `json:"private"`
	Unused  int `json:"unused"`
}

// Export is what gets marshaled to _meta/dead-functions.json.
type Export struct {
	Unused       []Function           `json:"unused"`
	UnusedTotal  int                  `json:"unused_total"`
	PrivateTotal int                  `json:"private_total"`
	Languages    map[string]LangStats `json:"languages"`
	// Unsupported counts source files in languages the scanner has no rules for, by extension.
	Unsupported map[string]int `json:"unsupported"`
}

// unsupportedExts are recognized as source code (so they can be reported) but not analyzed.
var unsupportedExts = map[string]bool{
	".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true, ".hpp": true, ".rb": true,
	".php": true, ".swift": true, ".scala": true, ".m": true, ".lua": true, ".dart": true,
	".ex": true, ".exs": true, ".hs": true, ".clj": true, ".pl": true, ".r": true, ".vue": true,
}

type def struct {
	file, name string
	line       int
	private    bool // counted in PrivateTotal and eligible to be reported
}

type langAcc struct {
	refs  map[string]int
	defs  map[string]int
	cands []def
	files int
}

// Scan walks repoDir and returns unused private functions for every supported language.
func Scan(repoDir string) (Export, error) {
	accs := make(map[string]*langAcc)
	export := Export{Languages: map[string]LangStats{}, Unsupported: map[string]int{}, Unused: []Function{}}

	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != repoDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		lang := languageFor(d.Name())
		if lang == nil {
			if unsupportedExts[ext] {
				export.Unsupported[ext]++
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil // one unreadable file must not fail the whole export
		}
		if bytes.IndexByte(src, 0) != -1 || isGenerated(d.Name(), src) {
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		acc := accs[lang.name]
		if acc == nil {
			acc = &langAcc{refs: map[string]int{}, defs: map[string]int{}}
			accs[lang.name] = acc
		}
		acc.files++
		code, strs := strip(src, lang)
		countIdents(code, acc.refs)
		countIdents(strs, acc.refs)
		isTest := lang.isTest(rel)
		for _, df := range findDefs(code, lang) {
			acc.defs[df.name]++
			if df.private && !isTest {
				df.file = rel
				acc.cands = append(acc.cands, df)
			}
		}
		return nil
	})
	if err != nil {
		return Export{}, err
	}

	for name, acc := range accs {
		stats := LangStats{Files: acc.files, Private: len(acc.cands)}
		for _, c := range acc.cands {
			// Every definition contributes one occurrence of its own name; anything beyond that is
			// a reference (call, method value, mention in a string).
			if acc.refs[c.name] > acc.defs[c.name] {
				continue
			}
			stats.Unused++
			export.Unused = append(export.Unused, Function{File: c.file, Line: c.line, Name: c.name, Lang: name})
		}
		export.Languages[name] = stats
		export.PrivateTotal += stats.Private
	}
	sort.Slice(export.Unused, func(i, j int) bool {
		a, b := export.Unused[i], export.Unused[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	export.UnusedTotal = len(export.Unused)
	if len(export.Unused) > MaxListed {
		export.Unused = export.Unused[:MaxListed]
	}
	return export, nil
}

var generatedSuffixes = []string{".pb.go", "_grpc.pb.go", ".g.cs", ".designer.cs", ".generated.cs", "_generated.go"}

func isGenerated(name string, src []byte) bool {
	lower := strings.ToLower(name)
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	head := src
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("Code generated")) || bytes.Contains(head, []byte("<auto-generated")) ||
		bytes.Contains(head, []byte("@generated"))
}

func countIdents(b []byte, refs map[string]int) {
	i := 0
	for i < len(b) {
		r, size := utf8.DecodeRune(b[i:])
		if r == '_' || unicode.IsLetter(r) {
			start := i
			for i < len(b) {
				r, size = utf8.DecodeRune(b[i:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				i += size
			}
			refs[string(b[start:i])]++
			continue
		}
		if unicode.IsDigit(r) { // skip number literals so 0x1f doesn't yield "x1f"
			for i < len(b) {
				r, size = utf8.DecodeRune(b[i:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				i += size
			}
			continue
		}
		i += size
	}
}

// findDefs runs the language's definition rules line by line over comment- and string-free code.
func findDefs(code []byte, lang *language) []def {
	var (
		defs        []def
		annotations []string
		// Rust: brace stack marking trait / trait-impl bodies, whose methods are called through
		// the trait and must not be reported.
		scopes      []bool
		pendingSkip bool
		annDepth    int
	)
	for n, raw := range strings.Split(string(code), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		inSkipped := len(scopes) > 0 && scopes[len(scopes)-1]
		if annDepth > 0 { // continuation of a multi-line annotation's arguments
			annDepth += strings.Count(line, "(") - strings.Count(line, ")")
			continue
		}
		if open, ok := annotationOnly(line, lang); ok {
			annotations = append(annotations, line)
			annDepth = open
			continue
		}
		if lang.skipScope != nil && lang.skipScope.MatchString(raw) {
			pendingSkip = true
		}
		if m := lang.def.FindStringSubmatchIndex(raw); m != nil {
			name := raw[m[2*lang.nameGroup]:m[2*lang.nameGroup+1]]
			priv := lang.isPrivate(raw, m) && !lang.excluded[name] && !inSkipped &&
				!hasEntryAnnotation(annotations, lang) && !hasEntryAnnotation([]string{raw[:m[0]]}, lang)
			if lang.validName == nil || lang.validName(name) {
				defs = append(defs, def{name: name, line: n + 1, private: priv})
			}
		}
		annotations = annotations[:0]
		if lang.skipScope != nil {
			for _, c := range raw {
				switch c {
				case '{':
					scopes = append(scopes, pendingSkip || (len(scopes) > 0 && scopes[len(scopes)-1]))
					pendingSkip = false
				case '}':
					if len(scopes) > 0 {
						scopes = scopes[:len(scopes)-1]
					}
				}
			}
		}
	}
	return defs
}

// annotationOnly reports whether line consists solely of annotations/attributes (so it decorates
// the next declaration, unlike `@Schema(...) String email,` in a record header), and how many
// parentheses are left open for a multi-line annotation argument list.
func annotationOnly(line string, lang *language) (open int, ok bool) {
	p := lang.annotationPrefix
	if p == "" || !strings.HasPrefix(line, p) {
		return 0, false
	}
	for line != "" {
		if !strings.HasPrefix(line, p) {
			return 0, false
		}
		i := len(p)
		if p == "@" { // @Name or @Name(args)
			for i < len(line) && (isIdentByte(line[i]) || line[i] == '.' || line[i] == ':') {
				i++
			}
			if i < len(line) && line[i] == '(' {
				depth := 0
				for ; i < len(line); i++ {
					if line[i] == '(' {
						depth++
					} else if line[i] == ')' {
						depth--
						if depth == 0 {
							i++
							break
						}
					}
				}
				if depth > 0 {
					return depth, true
				}
			}
		} else { // [Attr(args)] or #[attr(args)]
			depth := 1
			for ; i < len(line) && depth > 0; i++ {
				if line[i] == '[' {
					depth++
				} else if line[i] == ']' {
					depth--
				}
			}
			if depth > 0 {
				return strings.Count(line, "(") - strings.Count(line, ")"), true
			}
		}
		line = strings.TrimSpace(line[i:])
	}
	return 0, true
}

// hasEntryAnnotation reports whether any annotation/attribute is one that can make a framework or
// test harness call the function (anything not in the language's known-harmless list).
func hasEntryAnnotation(annotations []string, lang *language) bool {
	if lang.annotationRe == nil {
		return false
	}
	for _, a := range annotations {
		for _, tok := range lang.annotationRe.FindAllStringSubmatch(a, -1) {
			if !lang.harmless[tok[1]] {
				return true
			}
		}
	}
	return false
}
