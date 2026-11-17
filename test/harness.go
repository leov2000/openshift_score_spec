// Package test is the offline test harness for the score-k8s OpenShift bundle.
//
// Every test runs the pinned score-k8s binary (`init` + `generate`) against a fixture in a fresh temp
// directory, then strict-decodes the generated manifests into the typed structs from
// github.com/openshift/api and k8s.io/api. A misspelled or unknown field fails the decode.
package test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// BundleRoot is the root of the bundle repo (the parent of this test module).
func BundleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

// PinnedVersion is the score-k8s version the bundle is tested against, read from SCORE_K8S_VERSION.
func PinnedVersion() (string, error) {
	raw, err := os.ReadFile(filepath.Join(BundleRoot(), "SCORE_K8S_VERSION"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// ScoreBin locates the score-k8s binary: $SCORE_K8S_BIN, then <bundle>/.bin/score-k8s, then $PATH.
func ScoreBin() (string, error) {
	if v := os.Getenv("SCORE_K8S_BIN"); v != "" {
		return v, nil
	}
	local := filepath.Join(BundleRoot(), ".bin", "score-k8s")
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	return exec.LookPath("score-k8s")
}

// Run describes one init + generate invocation.
type Run struct {
	// Fixture is a directory under <bundle>/fixtures whose files are copied into the temp dir.
	Fixture string
	// Files are extra files (name -> content) written after the fixture; they override fixture files.
	Files map[string]string
	// ScoreFiles are passed to generate. Default: every *score.yaml in the temp dir, sorted.
	ScoreFiles []string
	// Stock disables the bundle: no custom provisioners or patch templates are installed.
	Stock bool
	// Namespace is passed as --namespace when non-empty.
	Namespace string
	// Dir reuses an existing project directory instead of creating a temp dir (init is skipped).
	Dir string
	// Env adds environment variables to both commands.
	Env []string
	// Sample lets init write its stock sample score.yaml (no --no-sample).
	Sample bool
}

// Output is the result of a successful generate.
type Output struct {
	Dir  string
	Raw  []byte
	Docs []Doc
}

// Doc is one generated manifest.
type Doc struct {
	APIVersion string
	Kind       string
	Name       string
	Raw        []byte
	Obj        map[string]interface{}
}

func (d Doc) String() string { return d.Kind + "/" + d.Name }

func runCmd(dir string, env []string, args ...string) (string, error) {
	bin, err := ScoreBin()
	if err != nil {
		return "", fmt.Errorf("score-k8s binary not found (set SCORE_K8S_BIN or run `make bin`): %w", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	return out.String(), err
}

// BundleInitArgs returns the `score-k8s init` arguments that install the bundle.
func BundleInitArgs() ([]string, error) {
	root := BundleRoot()
	args := []string{}
	provs, err := filepath.Glob(filepath.Join(root, "provisioners", "*.provisioners.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(provs)
	// init gives earlier --provisioners precedence, so list them in file order.
	for _, p := range provs {
		args = append(args, "--provisioners", p)
	}
	patches, err := filepath.Glob(filepath.Join(root, "patches", "*.tpl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(patches)
	for _, p := range patches {
		args = append(args, "--patch-templates", p)
	}
	return args, nil
}

// Prepare creates the project directory, copies the fixture and runs init. It returns the directory.
func Prepare(t testing.TB, r Run) string {
	t.Helper()
	if r.Dir != "" {
		return r.Dir
	}
	dir := t.TempDir()
	if r.Fixture != "" {
		src := filepath.Join(BundleRoot(), "fixtures", r.Fixture)
		if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
			t.Fatalf("copy fixture %s: %v", r.Fixture, err)
		}
	}
	for name, content := range r.Files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"init"}
	if !r.Sample {
		args = append(args, "--no-sample")
	}
	if !r.Stock {
		extra, err := BundleInitArgs()
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, extra...)
	}
	if out, err := runCmd(dir, r.Env, args...); err != nil {
		t.Fatalf("score-k8s init failed: %v\n%s", err, out)
	}
	return dir
}

func generateArgs(dir string, r Run) ([]string, error) {
	files := r.ScoreFiles
	if files == nil {
		matches, err := filepath.Glob(filepath.Join(dir, "*score.yaml"))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			files = append(files, filepath.Base(m))
		}
		sort.Strings(files)
	}
	args := append([]string{"generate"}, files...)
	args = append(args, "-o", "manifests.yaml")
	if r.Namespace != "" {
		args = append(args, "--namespace", r.Namespace)
	}
	return args, nil
}

// TryGenerate runs init + generate and returns the combined command output and error, for negative tests.
func TryGenerate(t testing.TB, r Run) (string, error) {
	t.Helper()
	dir := Prepare(t, r)
	args, err := generateArgs(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	return runCmd(dir, r.Env, args...)
}

// Generate runs init + generate and fails the test if either fails.
func Generate(t testing.TB, r Run) *Output {
	t.Helper()
	dir := Prepare(t, r)
	args, err := generateArgs(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runCmd(dir, r.Env, args...); err != nil {
		t.Fatalf("score-k8s generate failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifests.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	docs, err := SplitManifests(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &Output{Dir: dir, Raw: raw, Docs: docs}
}

// SplitManifests splits a multi-document YAML stream into Docs.
func SplitManifests(raw []byte) ([]Doc, error) {
	var docs []Doc
	var cur []string
	flush := func() error {
		body := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = nil
		if body == "" {
			return nil
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(body), &obj); err != nil {
			return fmt.Errorf("decode manifest: %w\n%s", err, body)
		}
		d := Doc{Raw: []byte(body + "\n"), Obj: obj}
		d.APIVersion, _ = obj["apiVersion"].(string)
		d.Kind, _ = obj["kind"].(string)
		if md, ok := obj["metadata"].(map[string]interface{}); ok {
			d.Name, _ = md["name"].(string)
		}
		docs = append(docs, d)
		return nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimRight(line, " ") == "---" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		cur = append(cur, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return docs, nil
}

// FindAll returns every manifest of the given kind.
func (o *Output) FindAll(kind string) []Doc {
	var out []Doc
	for _, d := range o.Docs {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

// Lookup returns the manifest with the given kind and name.
func (o *Output) Lookup(kind, name string) (Doc, bool) {
	for _, d := range o.Docs {
		if d.Kind == kind && d.Name == name {
			return d, true
		}
	}
	return Doc{}, false
}

// Find returns the manifest with the given kind and name or fails the test.
func (o *Output) Find(t testing.TB, kind, name string) Doc {
	t.Helper()
	d, ok := o.Lookup(kind, name)
	if !ok {
		t.Fatalf("no %s/%s in output; have: %v", kind, name, o.Docs)
	}
	return d
}

// Kinds lists "Kind/name" for every manifest, for assertions and failure messages.
func (o *Output) Kinds() []string {
	out := make([]string, 0, len(o.Docs))
	for _, d := range o.Docs {
		out = append(out, d.String())
	}
	return out
}

// Decode strict-decodes a manifest into T. Unknown fields fail the test.
func Decode[T any](t testing.TB, d Doc) *T {
	t.Helper()
	v := new(T)
	if err := yaml.UnmarshalStrict(d.Raw, v); err != nil {
		t.Fatalf("strict decode of %s into %T failed: %v\n%s", d, v, err, d.Raw)
	}
	return v
}

// Must finds a manifest by kind and name and strict-decodes it into T.
func Must[T any](t testing.TB, o *Output, kind, name string) *T {
	t.Helper()
	return Decode[T](t, o.Find(t, kind, name))
}

// DecodeAll strict-decodes every manifest whose kind has a typed struct registered in Kinds.
func DecodeAll(t testing.TB, o *Output) {
	t.Helper()
	for _, d := range o.Docs {
		f, ok := typedDecoders[d.APIVersion+"/"+d.Kind]
		if !ok {
			t.Errorf("no typed decoder registered for %s %s", d.APIVersion, d)
			continue
		}
		if err := f(d.Raw); err != nil {
			t.Errorf("strict decode of %s failed: %v\n%s", d, err, d.Raw)
		}
	}
}

// ErrContains reports whether err is non-nil and out mentions want.
func ErrContains(out string, err error, want string) error {
	if err == nil {
		return errors.New("expected generate to fail but it succeeded")
	}
	if !strings.Contains(out, want) {
		return fmt.Errorf("expected failure output to contain %q, got:\n%s", want, out)
	}
	return nil
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
