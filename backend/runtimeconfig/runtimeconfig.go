// Package runtimeconfig resolves service settings from four layers, highest first:
//
//  1. the process environment, snapshotted once at Init (secrets, docker-compose overrides);
//  2. admin overrides (set from the admin panel, delivered over Kafka);
//  3. config/app.yaml — the `shared` section plus the service's own sections;
//  4. the code default at the call site (what Getenv returning "" falls back to).
//
// The environment always wins: the snapshot is never written back to the process environment,
// and reloading app.yaml or receiving an override only rebuilds that one layer, so a key set in
// the environment (even to an empty string) keeps its environment value for the process lifetime.
//
// A YAML leaf key maps to the environment variable of the same name in upper case
// (`rate_limit` -> RATE_LIMIT), so services keep reading the names they always read: Getenv and
// LookupEnv are drop-in replacements for os.Getenv and os.LookupEnv. Keys under a section's
// `static:` subsection are restart-only: a reload that changes one keeps the old value and logs
// that a restart is needed. Before Init (unit tests, tools) both functions read the live process
// environment, exactly like the os functions they replace.
package runtimeconfig

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// SharedSection is read by every service in addition to its own sections.
const SharedSection = "shared"

// EditableSection lists the keys the admin panel may override; it is not a settings section.
const EditableSection = "admin_editable"

const staticKey = "static"

// Source names a layer a value came from.
type Source string

const (
	SourceEnv      Source = "env"
	SourceOverride Source = "override"
	SourceFile     Source = "file"
	SourceDefault  Source = "default"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Setting is one YAML leaf.
type Setting struct {
	// Path is "section.key" or "section.static.key"; overrides are addressed by it.
	Path    string
	Section string
	Env     string
	Value   string
	// Set is false for a key written without a value (`key:` or `key: ~`): it documents the
	// setting while leaving the code default in charge.
	Set    bool
	Static bool
}

// Editable describes a key the admin panel may override, with the bounds every value is checked
// against before it is stored (aggregator) and again before it is applied (each service).
type Editable struct {
	Path  string   `yaml:"key" json:"key"`
	Type  string   `yaml:"type" json:"type"` // int, float, duration, bool, string, enum
	Min   string   `yaml:"min,omitempty" json:"min,omitempty"`
	Max   string   `yaml:"max,omitempty" json:"max,omitempty"`
	Enum  []string `yaml:"values,omitempty" json:"values,omitempty"`
	Group string   `yaml:"group" json:"group"`
	// Title is the Russian label shown in the admin panel.
	Title string `yaml:"title" json:"title"`
}

// File is a parsed and validated app.yaml.
type File struct {
	// Settings holds every leaf of every section, by Path.
	Settings map[string]Setting
	Editable map[string]Editable
	hash     [32]byte
}

// Parse reads app.yaml content. It rejects anything a service could misread: non-scalar leaves,
// bad key names, nested sections other than `static`, admin_editable entries that point at a
// missing or static key, and default values outside their own bounds.
func Parse(data []byte) (*File, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse app.yaml: %w", err)
	}
	f := &File{Settings: map[string]Setting{}, Editable: map[string]Editable{}, hash: sha256.Sum256(data)}
	if len(root.Content) == 0 {
		return f, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, errors.New("app.yaml: top level must be a mapping of sections")
	}
	var editable []Editable
	for i := 0; i < len(doc.Content); i += 2 {
		name, body := doc.Content[i].Value, doc.Content[i+1]
		if name == EditableSection {
			if err := body.Decode(&editable); err != nil {
				return nil, fmt.Errorf("app.yaml %s: %w", EditableSection, err)
			}
			continue
		}
		if !keyPattern.MatchString(name) {
			return nil, fmt.Errorf("app.yaml: bad section name %q", name)
		}
		if err := f.addSection(name, body); err != nil {
			return nil, err
		}
	}
	for _, e := range editable {
		if err := f.addEditable(e); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *File) addSection(name string, body *yaml.Node) error {
	if body.Kind == yaml.ScalarNode && body.Tag == "!!null" {
		return nil
	}
	if body.Kind != yaml.MappingNode {
		return fmt.Errorf("app.yaml: section %s must be a mapping", name)
	}
	for i := 0; i < len(body.Content); i += 2 {
		key, value := body.Content[i].Value, body.Content[i+1]
		if key == staticKey {
			if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
				continue
			}
			if value.Kind != yaml.MappingNode {
				return fmt.Errorf("app.yaml: %s.static must be a mapping", name)
			}
			for j := 0; j < len(value.Content); j += 2 {
				if err := f.addLeaf(name, value.Content[j].Value, value.Content[j+1], true); err != nil {
					return err
				}
			}
			continue
		}
		if err := f.addLeaf(name, key, value, false); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) addLeaf(section, key string, value *yaml.Node, static bool) error {
	path := section + "." + key
	if static {
		path = section + "." + staticKey + "." + key
	}
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("app.yaml: bad key name %q", path)
	}
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("app.yaml: %s must be a single value (lists are comma-separated strings)", path)
	}
	env := strings.ToUpper(key)
	for _, s := range f.Settings {
		if s.Section == section && s.Env == env {
			return fmt.Errorf("app.yaml: %s and %s both set %s", s.Path, path, env)
		}
	}
	set := value.Tag != "!!null"
	v := value.Value
	if !set {
		v = ""
	}
	f.Settings[path] = Setting{Path: path, Section: section, Env: env, Value: v, Set: set, Static: static}
	return nil
}

func (f *File) addEditable(e Editable) error {
	s, ok := f.Settings[e.Path]
	if !ok {
		return fmt.Errorf("app.yaml %s: %s is not a setting", EditableSection, e.Path)
	}
	if s.Static {
		return fmt.Errorf("app.yaml %s: %s is static (restart-only) and cannot be edited at runtime", EditableSection, e.Path)
	}
	if _, dup := f.Editable[e.Path]; dup {
		return fmt.Errorf("app.yaml %s: %s listed twice", EditableSection, e.Path)
	}
	switch e.Type {
	case "int", "float", "duration", "bool", "string":
	case "enum":
		if len(e.Enum) == 0 {
			return fmt.Errorf("app.yaml %s: %s is an enum without values", EditableSection, e.Path)
		}
	default:
		return fmt.Errorf("app.yaml %s: %s has unknown type %q", EditableSection, e.Path, e.Type)
	}
	for _, bound := range []string{e.Min, e.Max} {
		if bound == "" {
			continue
		}
		if e.Type != "int" && e.Type != "float" && e.Type != "duration" {
			return fmt.Errorf("app.yaml %s: %s: min/max only apply to int, float and duration", EditableSection, e.Path)
		}
		if _, err := parseNumber(e.Type, bound); err != nil {
			return fmt.Errorf("app.yaml %s: %s bound %q: %w", EditableSection, e.Path, bound, err)
		}
	}
	if s.Set {
		if err := e.Validate(s.Value); err != nil {
			return fmt.Errorf("app.yaml: %s default: %w", e.Path, err)
		}
	}
	f.Editable[e.Path] = e
	return nil
}

// Validate checks an override value against the key's type and bounds.
func (e Editable) Validate(value string) error {
	switch e.Type {
	case "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%q is not true/false", value)
		}
		return nil
	case "string":
		return nil
	case "enum":
		for _, v := range e.Enum {
			if v == value {
				return nil
			}
		}
		return fmt.Errorf("%q is not one of %s", value, strings.Join(e.Enum, ", "))
	}
	n, err := parseNumber(e.Type, value)
	if err != nil {
		return err
	}
	if e.Min != "" {
		if min, _ := parseNumber(e.Type, e.Min); n < min {
			return fmt.Errorf("%s is below the minimum %s", value, e.Min)
		}
	}
	if e.Max != "" {
		if max, _ := parseNumber(e.Type, e.Max); n > max {
			return fmt.Errorf("%s is above the maximum %s", value, e.Max)
		}
	}
	return nil
}

func parseNumber(typ, value string) (float64, error) {
	switch typ {
	case "int":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("%q is not a whole number", value)
		}
		return float64(n), nil
	case "float":
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", value)
		}
		return n, nil
	case "duration":
		d, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("%q is not a duration (e.g. 90s, 10m, 2h)", value)
		}
		return float64(d), nil
	}
	return 0, fmt.Errorf("type %s has no numeric value", typ)
}

// view is the resolved file layer of one service: env name -> setting.
type view map[string]Setting

func (f *File) view(sections []string) (view, error) {
	v := view{}
	for _, section := range sections {
		for _, s := range f.Settings {
			if s.Section != section {
				continue
			}
			if prev, dup := v[s.Env]; dup {
				return nil, fmt.Errorf("app.yaml: %s and %s both set %s for the same service", prev.Path, s.Path, s.Env)
			}
			v[s.Env] = s
		}
	}
	return v, nil
}

// Store is one service's resolved configuration.
type Store struct {
	path     string
	sections []string
	env      map[string]string

	file atomic.Pointer[File]
	view atomic.Pointer[view]

	// badHash is the last content that failed to parse, so a broken file is reported once.
	badHash atomic.Pointer[[32]byte]

	mu        sync.Mutex
	overrides map[string]string // Path -> value
	listeners []func()
	// pending holds static keys whose file value changed after startup, for the restart warning.
	pending map[string]struct{}
}

var global atomic.Pointer[Store]

// Init loads path for service (plus any extra sections, e.g. gateway also reads `frontend` to
// serve it), snapshots the environment and makes the package functions use the result. A missing
// file is not an error — every key then comes from the environment or the code default — but an
// invalid one is: a service must not start on a config it misreads.
func Init(path, service string, extra ...string) (*Store, error) {
	s, err := New(path, service, extra...)
	if err != nil {
		return nil, err
	}
	global.Store(s)
	s.logSources()
	return s, nil
}

// New builds a Store without installing it as the package default (tests).
func New(path, service string, extra ...string) (*Store, error) {
	s := &Store{
		path:      path,
		sections:  append([]string{SharedSection, service}, extra...),
		env:       snapshotEnv(),
		overrides: map[string]string{},
		pending:   map[string]struct{}{},
	}
	f, err := readFile(path)
	if err != nil {
		return nil, err
	}
	v, err := f.view(s.sections)
	if err != nil {
		return nil, err
	}
	s.file.Store(f)
	s.view.Store(&v)
	return s, nil
}

func readFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("config: %s not found, settings come from the environment and code defaults", path)
		return Parse(nil)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

func snapshotEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// LookupEnv resolves key through the layers; ok is false only when no layer sets it.
func (s *Store) LookupEnv(key string) (string, bool) {
	value, src := s.resolve(key)
	return value, src != SourceDefault
}

// Getenv resolves key through the layers; "" means the code default applies.
func (s *Store) Getenv(key string) string {
	value, _ := s.resolve(key)
	return value
}

// Source reports which layer key's value comes from.
func (s *Store) Source(key string) Source {
	_, src := s.resolve(key)
	return src
}

func (s *Store) resolve(key string) (string, Source) {
	if v, ok := s.env[key]; ok {
		return v, SourceEnv
	}
	setting, ok := (*s.view.Load())[key]
	if !ok {
		return "", SourceDefault
	}
	if !setting.Static {
		s.mu.Lock()
		v, overridden := s.overrides[setting.Path]
		s.mu.Unlock()
		if overridden {
			return v, SourceOverride
		}
	}
	if !setting.Set {
		return "", SourceDefault
	}
	return setting.Value, SourceFile
}

// OnChange registers fn to run after any reload or override change that may alter a value. It
// runs on the goroutine that applied the change, so it must not block.
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

func (s *Store) notify() {
	s.mu.Lock()
	listeners := append([]func(){}, s.listeners...)
	s.mu.Unlock()
	for _, fn := range listeners {
		fn()
	}
}

// Reload re-reads the file. An invalid file keeps the last good config (the error is returned
// for logging); static keys keep their startup values. It reports whether anything changed.
func (s *Store) Reload() (bool, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = nil, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", s.path, err)
	}
	sum := sha256.Sum256(data)
	if sum == s.file.Load().hash {
		return false, nil
	}
	if bad := s.badHash.Load(); bad != nil && *bad == sum {
		return false, nil
	}
	f, err := Parse(data)
	var next view
	if err == nil {
		next, err = f.view(s.sections)
	}
	if err != nil {
		s.badHash.Store(&sum)
		return false, err
	}
	old := *s.view.Load()
	// Static keys keep the value the service started with.
	for env, prev := range old {
		if !prev.Static {
			continue
		}
		if cur, ok := next[env]; !ok || !cur.Static || cur.Set != prev.Set || cur.Value != prev.Value {
			s.mu.Lock()
			s.pending[prev.Path] = struct{}{}
			s.mu.Unlock()
			log.Printf("config: %s changed in %s; it is restart-only, the running value is kept until restart", prev.Path, s.path)
		} else {
			// Back to the value the process started with: no restart needed any more.
			s.mu.Lock()
			delete(s.pending, prev.Path)
			s.mu.Unlock()
		}
		next[env] = prev
	}
	for env, cur := range next {
		if prev, existed := old[env]; cur.Static && (!existed || !prev.Static) {
			if existed {
				next[env] = prev
			} else {
				delete(next, env)
			}
			log.Printf("config: %s became restart-only in %s; it applies after restart", cur.Path, s.path)
		}
	}
	s.file.Store(f)
	s.view.Store(&next)
	// An override whose key stopped being editable is dropped with the file that removed it.
	s.mu.Lock()
	for path := range s.overrides {
		if _, ok := f.Editable[path]; !ok {
			delete(s.overrides, path)
			log.Printf("config: override of %s dropped, the key is no longer admin-editable", path)
		}
	}
	s.mu.Unlock()
	log.Printf("config: reloaded %s", s.path)
	s.notify()
	return true, nil
}

// Sections lists the sections this store reads, `shared` first.
func (s *Store) Sections() []string { return append([]string(nil), s.sections...) }

// File returns the last successfully loaded file.
func (s *Store) File() *File { return s.file.Load() }

// Owns reports whether path belongs to one of this store's sections.
func (s *Store) Owns(path string) bool {
	section, _, _ := strings.Cut(path, ".")
	for _, own := range s.sections {
		if own == section {
			return true
		}
	}
	return false
}

// SetOverride applies an admin override. It is ignored (with the reason returned) for keys of
// other services, non-editable keys, invalid values — and keys set in the environment, which
// always win, so nothing is stored for them.
func (s *Store) SetOverride(path, value string) error {
	if !s.Owns(path) {
		return nil
	}
	e, ok := s.File().Editable[path]
	if !ok {
		return fmt.Errorf("%s is not admin-editable", path)
	}
	if err := e.Validate(value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if s.EnvLocked(path) {
		return fmt.Errorf("%s is set in the environment, which always wins", path)
	}
	s.mu.Lock()
	prev, had := s.overrides[path]
	s.overrides[path] = value
	s.mu.Unlock()
	if !had || prev != value {
		log.Printf("config: override applied to %s", path)
		s.notify()
	}
	return nil
}

// DeleteOverride drops an override (admin reset); the file value applies again.
func (s *Store) DeleteOverride(path string) {
	s.mu.Lock()
	_, had := s.overrides[path]
	delete(s.overrides, path)
	s.mu.Unlock()
	if had {
		log.Printf("config: override of %s reset to the file value", path)
		s.notify()
	}
}

// Overrides returns a copy of the applied overrides.
func (s *Store) Overrides() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.overrides))
	for k, v := range s.overrides {
		out[k] = v
	}
	return out
}

// EnvLocked reports whether path's key is set in this process's environment.
func (s *Store) EnvLocked(path string) bool {
	setting, ok := s.File().Settings[path]
	if !ok {
		return false
	}
	_, inEnv := s.env[setting.Env]
	return inEnv
}

// EnvLockedPaths lists this service's settings whose key is set in the environment.
func (s *Store) EnvLockedPaths() []string {
	var out []string
	for _, setting := range *s.view.Load() {
		if _, ok := s.env[setting.Env]; ok {
			out = append(out, setting.Path)
		}
	}
	sort.Strings(out)
	return out
}

// RestartPending lists static keys changed in the file since startup.
func (s *Store) RestartPending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.pending))
	for p := range s.pending {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Sources lists every setting of this service with the layer its value comes from. It carries
// names only, never values, so it is safe to log and to show to admins.
func (s *Store) Sources() map[string]Source {
	out := map[string]Source{}
	for env, setting := range *s.view.Load() {
		out[setting.Path] = s.Source(env)
	}
	return out
}

func (s *Store) logSources() {
	sources := s.Sources()
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, p+"="+string(sources[p]))
	}
	log.Printf("config: %s loaded, sources: %s", s.path, strings.Join(parts, " "))
}

// Default returns the store installed by Init, or nil.
func Default() *Store { return global.Load() }

// Getenv is os.Getenv through the installed store; before Init it is os.Getenv.
func Getenv(key string) string {
	if s := global.Load(); s != nil {
		return s.Getenv(key)
	}
	return os.Getenv(key)
}

// LookupEnv is os.LookupEnv through the installed store; before Init it is os.LookupEnv.
func LookupEnv(key string) (string, bool) {
	if s := global.Load(); s != nil {
		return s.LookupEnv(key)
	}
	return os.LookupEnv(key)
}

// OnChange registers fn on the installed store; a no-op before Init.
func OnChange(fn func()) {
	if s := global.Load(); s != nil {
		s.OnChange(fn)
	}
}

// Path returns the app.yaml location: APP_CONFIG from the environment, else config/app.yaml
// relative to the working directory (the repo root on the host, /app in the image).
func Path() string {
	if p := os.Getenv("APP_CONFIG"); p != "" {
		return p
	}
	return "config/app.yaml"
}

// Services lists the sections each process reads besides `shared`; Start's arguments must match.
// gateway also reads `frontend` because it serves those settings at GET /api/config.
var Services = map[string][]string{
	"aggregator":          {"aggregator"},
	"gateway":             {"gateway", "frontend"},
	"rating":              {"rating"},
	"repository":          {"repository"},
	"grade":               {"grade"},
	"security_wrapper":    {"security_wrapper"},
	"sourcecraft_wrapper": {"sourcecraft_wrapper"},
	"llm":                 {"llm"},
}
