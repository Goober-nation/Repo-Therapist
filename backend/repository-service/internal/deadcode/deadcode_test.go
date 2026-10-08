package deadcode

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func unusedNames(t *testing.T, files map[string]string) ([]string, Export) {
	t.Helper()
	e, err := Scan(writeTree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range e.Unused {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names, e
}

func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("unused = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unused = %v, want %v", got, want)
		}
	}
}

func TestGo(t *testing.T) {
	got, e := unusedNames(t, map[string]string{
		"a.go": "package a\n\nfunc main() { used() }\nfunc init() {}\n" +
			"func used() { var f = viaValue; f() }\nfunc viaValue() {}\n" +
			"func dead() {}\n" +
			"// deadInComment() mentioned only in a comment\nfunc deadInComment() {}\n" +
			"func Exported() {}\n" +
			"func (s *T) method() {}\n" +
			"func generic[T any](x T) {}\n" +
			"func reflected() {}\nvar name = \"reflected\"\n" +
			"var raw = `/* not a comment */ x`\nfunc afterRaw() {}\n",
		"a_test.go": "package a\nfunc helperOnlyInTest() {}\nfunc TestX() { method2() }\n",
		"b.go":      "package a\nfunc method2() {}\n",
		"gen.pb.go": "package a\nfunc generatedDead() {}\n",
	})
	assertNames(t, got, "afterRaw", "dead", "deadInComment", "generic", "method")
	// used, viaValue, dead, deadInComment, method, generic, reflected, afterRaw, method2
	if e.Languages["go"].Private != 9 {
		t.Fatalf("private = %d, want 9", e.Languages["go"].Private)
	}
}

func TestJava(t *testing.T) {
	got, _ := unusedNames(t, map[string]string{
		"src/main/java/A.java": `class A {
    private static final String S = helper("x");
    private A() {}
    public void run() { used(); Runnable r = this::byRef; }
    private void used() {}
    private void byRef() {}
    private void dead() {}
    private static <T> java.util.List<T> deadGeneric(java.util.Map<String, T> m) { return null; }
    @PostConstruct
    private void initByFramework() {}
    @SuppressWarnings("unused")
    private void deadSuppressed() {}
    @EventListener(
        value = Foo.class
    )
    private void multiLineAnnotation() {}
    private void readObject(java.io.ObjectInputStream in) {}
    private native void nat();
    private static String helper(String s) { return "//" + s; }
    private void afterUrlString() {}
}`,
		"src/test/java/ATest.java": "class ATest { private void testOnly() {} }",
	})
	assertNames(t, got, "afterUrlString", "dead", "deadGeneric", "deadSuppressed")
}

func TestKotlin(t *testing.T) {
	got, _ := unusedNames(t, map[string]string{
		"A.kt": `class A {
    fun run() { used(); println("${interp()}") }
    private fun used() {}
    private fun interp() = 1
    private fun dead() {}
    private suspend fun deadSuspend() {}
    private fun String.deadExt(): Int = 1
    private operator fun plus(o: A) = this
    @Test
    private fun annotated() {}
    /* nested /* comment */ still comment fun x() */
}`,
	})
	assertNames(t, got, "dead", "deadExt", "deadSuspend")
}

func TestCSharp(t *testing.T) {
	got, _ := unusedNames(t, map[string]string{
		"Src/A.cs": `class A {
    public void Run() { Used(); var s = $"{Interp()}"; }
    private void Used() {}
    private int Interp() => 1;
    private void Dead() {}
    private async Task<int> DeadAsync() { return 1; }
    private void Update() {}
    [HttpGet]
    private void Routed() {}
    [MethodImpl(MethodImplOptions.AggressiveInlining)]
    private void DeadInlined() {}
    private static extern int Native();
    private string V = @"C:\path\" + @"say ""hi"" // x";
    private void AfterVerbatim() {}
}`,
		"A.Tests/T.cs": "class T { private void Only() {} }",
	})
	assertNames(t, got, "AfterVerbatim", "Dead", "DeadAsync", "DeadInlined")
}

func TestRust(t *testing.T) {
	got, _ := unusedNames(t, map[string]string{
		"src/main.rs": `fn main() { used(); let s = r#"raw "quote" fn fake()"#; let c = '"'; }
fn used() {}
fn dead() {}
pub fn exported() {}
pub(crate) fn crate_visible() {}
async fn dead_async() {}
fn with_lifetime<'a>(x: &'a str) -> &'a str { x }
struct S;
impl S {
    fn dead_method(&self) {}
}
trait Tr {
    fn required(&self);
    fn provided(&self) {}
}
impl Tr for S {
    fn required(&self) {}
}
impl std::fmt::Display for S
where S: Sized
{
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result { Ok(()) }
}
extern "C" {
    fn c_fn();
}
#[test]
fn a_test() {}
#[inline]
fn dead_inline() {}
`,
		"tests/it.rs": "fn helper() {}",
	})
	assertNames(t, got, "dead", "dead_async", "dead_inline", "dead_method", "with_lifetime")
}

func TestLanguagesAreSeparateAndUnsupportedCounted(t *testing.T) {
	got, e := unusedNames(t, map[string]string{
		"a.go":   "package a\nfunc shared() {}\n",
		"B.java": "class B { void x() { shared(); } }",
		"c.py":   "def shared(): pass",
		"d.ts":   "",
	})
	assertNames(t, got, "shared")
	if e.Unsupported[".py"] != 1 || e.Unsupported[".ts"] != 1 {
		t.Fatalf("unsupported = %v", e.Unsupported)
	}
}

func TestSkippedDirsAndGenerated(t *testing.T) {
	got, e := unusedNames(t, map[string]string{
		"vendor/x/a.go":       "package x\nfunc dead() {}\n",
		"node_modules/a/b.go": "package x\nfunc dead() {}\n",
		"gen.go":              "// Code generated by foo. DO NOT EDIT.\npackage x\nfunc dead() {}\n",
	})
	assertNames(t, got)
	if e.PrivateTotal != 0 {
		t.Fatalf("private = %d, want 0", e.PrivateTotal)
	}
}

func TestAnnotatedRecordComponentsDoNotDecorateNextMethod(t *testing.T) {
	got, _ := unusedNames(t, map[string]string{
		"R.java": `public record R(
        @Schema(required = true) String a,
        @Schema(required = true) String b) {
    private void dead() {}
    @Bean @Primary
    private void bean() {}
}`,
	})
	assertNames(t, got, "dead")
}
