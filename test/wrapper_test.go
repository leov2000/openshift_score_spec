package test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

var fullScoreFiles = []string{"web/score.yaml", "cache/score.yaml"}

// copyFixture copies a fixture into a fresh directory, like a clean checkout.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(BundleRoot(), "fixtures", name))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runWrapper runs bin/score-ocp in dir with the pinned score-k8s and returns combined output.
func runWrapper(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	bin, err := ScoreBin()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{filepath.Join(BundleRoot(), "bin", "score-ocp")}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "SCORE_K8S="+bin), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	return out.String(), err
}

func TestEveryObjectCarriesProjectLabel(t *testing.T) {
	out := Generate(t, Run{Fixture: "full", ScoreFiles: fullScoreFiles, Env: []string{"SCORE_OCP_PROJECT=shop"}})
	if len(out.Docs) < 10 {
		t.Fatalf("suspiciously few objects: %v", out.Kinds())
	}
	for _, d := range out.Docs {
		var m struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal(d.Raw, &m); err != nil {
			t.Fatal(err)
		}
		if got := m.Metadata.Labels["app.kubernetes.io/part-of"]; got != "shop" {
			t.Errorf("%s: part-of = %q", d, got)
		}
	}
}

func TestProjectLabelAbsentWithoutEnv(t *testing.T) {
	out := Generate(t, Run{Fixture: "baseline"})
	if bytes.Contains(out.Raw, []byte("app.kubernetes.io/part-of")) {
		t.Error("part-of label set without SCORE_OCP_PROJECT")
	}
}

// Two clean checkouts, and a re-run in the same directory, all produce identical output: stateless CI is safe.
func TestWrapperOutputIdenticalAcrossCleanRuns(t *testing.T) {
	var outputs [][]byte
	for i := 0; i < 2; i++ {
		dir := copyFixture(t, "full")
		args := append([]string{"-p", "shop", "-n", "shop-dev", "--generate-only"}, fullScoreFiles...)
		if out, err := runWrapper(t, dir, nil, args...); err != nil {
			t.Fatalf("wrapper failed: %v\n%s", err, out)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "manifests.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, raw)

		if i == 0 {
			// Re-running in the same directory (init again over existing state) changes nothing.
			if out, err := runWrapper(t, dir, nil, args...); err != nil {
				t.Fatalf("wrapper re-run failed: %v\n%s", err, out)
			}
			again, _ := os.ReadFile(filepath.Join(dir, "manifests.yaml"))
			if !bytes.Equal(raw, again) {
				t.Errorf("re-run in same dir changed output:\n--- first\n%s\n--- again\n%s", raw, again)
			}
		}
	}
	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Fatalf("clean runs differ:\n--- a\n%s\n--- b\n%s", outputs[0], outputs[1])
	}
	if !bytes.Contains(outputs[0], []byte("namespace: shop-dev")) {
		t.Error("namespace not applied")
	}
}

func TestWrapperRejectsBadInput(t *testing.T) {
	dir := copyFixture(t, "full")
	cases := map[string][]string{
		"missing project": {"--generate-only", "web/score.yaml"},
		"bad project":     {"-p", "Shop_1", "--generate-only", "web/score.yaml"},
		"no score files":  {"-p", "shop", "--generate-only"},
		"missing file":    {"-p", "shop", "--generate-only", "nope.yaml"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if out, err := runWrapper(t, dir, nil, args...); err == nil {
				t.Fatalf("wrapper accepted bad input:\n%s", out)
			}
		})
	}
}

// fakeOC writes an `oc` stub that logs each call and answers `get`: BuildConfigs exist for the names in
// haveBC, ImageStreamTags exist for the names in haveIST.
func fakeOC(t *testing.T, haveBC, haveIST []string) (ocPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "oc.log")
	script := `#!/usr/bin/env bash
echo "$*" >> "` + logPath + `"
if [ "$1" = get ]; then
  case "$2 $3" in
` + func() string {
		var b strings.Builder
		for _, n := range haveBC {
			b.WriteString("    \"buildconfig " + n + "\") exit 0 ;;\n")
		}
		for _, n := range haveIST {
			b.WriteString("    \"imagestreamtag " + n + ":latest\") exit 0 ;;\n")
		}
		return b.String()
	}() + `    *) exit 1 ;;
  esac
fi
# Real oc exits non-zero for a failed build only with --wait; mimic that.
if [ "$1" = start-build ] && [ -n "${FAKE_OC_FAIL_BUILD:-}" ]; then
  case " $* " in *" --wait "*) exit 1 ;; esac
fi
exit 0
`
	ocPath = filepath.Join(dir, "oc")
	if err := os.WriteFile(ocPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return ocPath, logPath
}

func readLog(t *testing.T, p string) []string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestWrapperAppliesWithPruneAndStartsFirstBuild(t *testing.T) {
	dir := copyFixture(t, "full")
	oc, logPath := fakeOC(t, []string{"web"}, nil)
	args := append([]string{"-p", "shop", "-n", "shop-dev"}, fullScoreFiles...)
	if out, err := runWrapper(t, dir, []string{"OC=" + oc}, args...); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	calls := readLog(t, logPath)
	apply := calls[0]
	for _, want := range []string{
		"apply -f manifests.yaml --prune -l app.kubernetes.io/part-of=shop",
		"--prune-allowlist route.openshift.io/v1/Route",
		"--prune-allowlist build.openshift.io/v1/BuildConfig",
		"--prune-allowlist image.openshift.io/v1/ImageStream",
		"--prune-allowlist apps/v1/StatefulSet",
		"-n shop-dev",
	} {
		if !strings.Contains(apply, want) {
			t.Errorf("apply call missing %q:\n%s", want, apply)
		}
	}
	if strings.Contains(apply, "PersistentVolumeClaim") {
		t.Errorf("PVCs must not be pruned by default:\n%s", apply)
	}
	if strings.Contains(apply, "--dry-run") {
		t.Errorf("unexpected dry-run:\n%s", apply)
	}
	var builds []string
	for _, c := range calls {
		if strings.HasPrefix(c, "start-build") {
			builds = append(builds, c)
		}
	}
	if len(builds) != 1 || builds[0] != "start-build web --from-dir=web --follow --wait -n shop-dev" {
		t.Errorf("start-build calls = %q", builds)
	}
}

func TestWrapperSkipsBuildWhenTagExists(t *testing.T) {
	dir := copyFixture(t, "full")
	oc, logPath := fakeOC(t, []string{"web"}, []string{"web"})
	if out, err := runWrapper(t, dir, []string{"OC=" + oc}, append([]string{"-p", "shop"}, fullScoreFiles...)...); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	for _, c := range readLog(t, logPath) {
		if strings.HasPrefix(c, "start-build") {
			t.Errorf("unexpected %q", c)
		}
	}
}

func TestWrapperDryRunStopsAfterApply(t *testing.T) {
	dir := copyFixture(t, "full")
	oc, logPath := fakeOC(t, []string{"web"}, nil)
	if out, err := runWrapper(t, dir, []string{"OC=" + oc, "SCORE_OCP_PRUNE_PVC=1"}, append([]string{"-p", "shop", "--dry-run"}, fullScoreFiles...)...); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	calls := readLog(t, logPath)
	if len(calls) != 1 || !strings.Contains(calls[0], "--dry-run=server") || !strings.Contains(calls[0], "core/v1/PersistentVolumeClaim") {
		t.Errorf("calls = %q", calls)
	}
}

// Found on a live cluster: `oc start-build --follow` exits 0 when the build fails; the wrapper must pass --wait so a failed
// first build fails the run.
func TestWrapperFailsWhenFirstBuildFails(t *testing.T) {
	dir := copyFixture(t, "full")
	oc, _ := fakeOC(t, []string{"web"}, nil)
	out, err := runWrapper(t, dir, []string{"OC=" + oc, "FAKE_OC_FAIL_BUILD=1"}, append([]string{"-p", "shop"}, fullScoreFiles...)...)
	if err == nil {
		t.Fatalf("wrapper succeeded although the first build failed:\n%s", out)
	}
}

// Found on a live cluster: score-k8s accumulates workloads in .score-k8s/state.yaml, so a workload dropped from the command
// line was still generated (and never pruned). The wrapper must generate exactly the files it is given and leave
// no state behind.
func TestWrapperGeneratesOnlyGivenFiles(t *testing.T) {
	dir := copyFixture(t, "full")
	gen := func(files ...string) []Doc {
		t.Helper()
		if out, err := runWrapper(t, dir, nil, append([]string{"-p", "shop", "--generate-only"}, files...)...); err != nil {
			t.Fatalf("wrapper failed: %v\n%s", err, out)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "manifests.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		docs, err := SplitManifests(raw)
		if err != nil {
			t.Fatal(err)
		}
		return docs
	}
	gen(fullScoreFiles...)
	for _, d := range gen("cache/score.yaml") {
		if d.Name == "web" || d.Name == "web-route" || d.Name == "ubi9-node" {
			t.Errorf("dropped workload still generated: %s", d)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".score-k8s")); !os.IsNotExist(err) {
		t.Errorf("wrapper left .score-k8s in the working directory (err=%v)", err)
	}
}

// Relative files: sources still resolve when the wrapper runs score-k8s from a temporary directory.
func TestWrapperKeepsRelativeFileSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "app", "conf", "app.ini"), []byte("mode=prod\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "app", "score.yaml"), []byte(`apiVersion: score.dev/v1b1
metadata:
  name: app
containers:
  main:
    image: nginx:1.27
    files:
      /etc/app/app.ini:
        source: conf/app.ini
`), 0o644)
	if out, err := runWrapper(t, dir, nil, "-p", "shop", "--generate-only", "-o", "out/manifests.yaml", "app/score.yaml"); err != nil {
		t.Fatalf("wrapper failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "out", "manifests.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// score-k8s stores file content as ConfigMap binaryData: base64("mode=prod\n").
	if !bytes.Contains(raw, []byte("bW9kZT1wcm9kCg==")) {
		t.Errorf("file content not in manifests:\n%s", raw)
	}
}
