package test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The examples under examples/ are documentation, so they must keep working: every example generates through the
// wrapper and strict-decodes, and every file under examples/failures fails with the message in its "# expect:"
// line. examples/verify-live.sh checks the same examples on a real cluster.

func examplesDir() string { return filepath.Join(BundleRoot(), "examples") }

// exampleSets returns example name -> its Score files, for every examples/<name>/ with Score files (except failures).
func exampleSets(t *testing.T) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(examplesDir())
	if err != nil {
		t.Fatal(err)
	}
	sets := map[string][]string{}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "failures" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(examplesDir(), e.Name(), "*score.yaml"))
		sort.Strings(files)
		if len(files) > 0 {
			sets[e.Name()] = files
		}
	}
	if len(sets) < 9 {
		t.Fatalf("found only %d examples: %v", len(sets), sets)
	}
	return sets
}

func TestExamplesGenerateAndDecode(t *testing.T) {
	for name, files := range exampleSets(t) {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "manifests.yaml")
			args := append([]string{"-p", name, "--generate-only", "-o", out}, files...)
			if log, err := runWrapper(t, t.TempDir(), nil, args...); err != nil {
				t.Fatalf("wrapper failed: %v\n%s", err, log)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			docs, err := SplitManifests(raw)
			if err != nil {
				t.Fatal(err)
			}
			o := &Output{Raw: raw, Docs: docs}
			DecodeAll(t, o)
			workloads := len(o.FindAll("Deployment")) + len(o.FindAll("StatefulSet"))
			if workloads != len(files) {
				t.Errorf("%d Score files but %d workloads: %v", len(files), workloads, o.Kinds())
			}
			// Examples must run on any cluster: no hardcoded Route hosts.
			for _, r := range o.FindAll("Route") {
				if bytes.Contains(r.Raw, []byte("\n  host:")) {
					t.Errorf("%s sets spec.host; leave it to the router so the example is portable", r)
				}
			}
		})
	}
}

var expectLine = regexp.MustCompile(`(?m)^# expect: (.+)$`)

func TestFailureExamplesFailAsDocumented(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(examplesDir(), "failures", "*.score.yaml"))
	if len(files) < 10 {
		t.Fatalf("found only %d failure examples", len(files))
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".score.yaml"), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			m := expectLine.FindSubmatch(raw)
			if m == nil {
				t.Fatal("no '# expect: <message>' line")
			}
			out, err := runWrapper(t, t.TempDir(), nil, "-p", "x", "--generate-only", "-o", filepath.Join(t.TempDir(), "m.yaml"), f)
			if e := ErrContains(out, err, string(m[1])); e != nil {
				t.Fatal(e)
			}
		})
	}
}

// examples/quickstart bakes its own copy of the demo app into the image (oc start-build uploads only that
// directory); it must not drift from examples/app/app.py, which the other examples mount.
func TestQuickstartAppMatchesSharedApp(t *testing.T) {
	a, err := os.ReadFile(filepath.Join(examplesDir(), "app", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(examplesDir(), "quickstart", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("examples/quickstart/app.py differs from examples/app/app.py; copy it over")
	}
}
