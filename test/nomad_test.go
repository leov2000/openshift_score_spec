package test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func nomadBin(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("SCORE_NOMAD_BIN"); v != "" {
		return v
	}
	local := filepath.Join(BundleRoot(), ".bin", "score-nomad")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if p, err := exec.LookPath("score-nomad"); err == nil {
		return p
	}
	t.Skip("score-nomad not found (set SCORE_NOMAD_BIN or run `make bin`)")
	return ""
}

// nomadGenerate runs score-nomad init + generate with the bundle's parity provisioners in a fresh dir.
func nomadGenerate(t *testing.T, files map[string]string, scoreFiles ...string) (string, error) {
	t.Helper()
	bin := nomadBin(t)
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err
	}
	if out, err := run("init", "--no-sample"); err != nil {
		t.Fatalf("score-nomad init: %v\n%s", err, out)
	}
	parity, err := os.ReadFile(filepath.Join(BundleRoot(), "nomad", "10-openshift-parity.provisioners.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".score-nomad", "10-openshift-parity.provisioners.yaml"), parity, 0o644); err != nil {
		t.Fatal(err)
	}
	return run(append(append([]string{"generate"}, scoreFiles...), "-o", "out/")...)
}

const unknownTypeScore = `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  main:
    image: nginx:1.27
resources:
  thing:
    type: made-up-type
`

// An unknown resource type fails loudly on both targets, never silently skipped.
func TestUnknownTypeFailsOnBothTargets(t *testing.T) {
	out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": unknownTypeScore}})
	if e := ErrContains(out, err, "is not supported by any provisioner"); e != nil {
		t.Errorf("score-k8s: %v", e)
	}
	out, err = nomadGenerate(t, map[string]string{"score.yaml": unknownTypeScore}, "score.yaml")
	if e := ErrContains(out, err, "no provisioner matches type 'made-up-type'"); e != nil {
		t.Errorf("score-nomad: %v", e)
	}
}

// A portable subset (route with a numeric port) generates on both targets.
func TestRouteGeneratesOnBothTargets(t *testing.T) {
	score := routeScore("", "      host: hello.apps.example.com\n      path: /\n      port: 80\n")
	Generate(t, Run{Files: map[string]string{"score.yaml": score}})
	if out, err := nomadGenerate(t, map[string]string{"score.yaml": score}, "score.yaml"); err != nil {
		t.Fatalf("score-nomad generate failed: %v\n%s", err, out)
	}
}

// Recorded parity gap: score-nomad's traefik route rejects a port name that the OpenShift bundle accepts.
func TestNomadRejectsRoutePortName(t *testing.T) {
	score := routeScore("", "      host: hello.apps.example.com\n      path: /\n      port: web\n")
	out, err := nomadGenerate(t, map[string]string{"score.yaml": score}, "score.yaml")
	if err == nil {
		t.Fatalf("score-nomad now accepts port names; update the note in nomad/10-openshift-parity.provisioners.yaml\n%s", out)
	}
	if e := ErrContains(out, err, "provisioner 'traefik'"); e != nil {
		t.Fatal(e)
	}
}

// The full-feature fixture cannot generate on Nomad until score-nomad can express an explicit no-op.
func TestFullFixtureOnBothTargets(t *testing.T) {
	Generate(t, Run{Fixture: "full", ScoreFiles: fullScoreFiles})
	t.Skip("BLOCKED: openshift-build, openshift-secret, hpa and service-port have no score-nomad provisioner " +
		"(no no-op builtin). See nomad/10-openshift-parity.provisioners.yaml.")
}
