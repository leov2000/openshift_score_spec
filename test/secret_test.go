package test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func secretRefs(c corev1.Container) []string {
	var out []string
	for _, e := range c.EnvFrom {
		if e.SecretRef != nil {
			out = append(out, e.SecretRef.Name)
		}
	}
	return out
}

func TestEnvFromPerSecret(t *testing.T) {
	out := Generate(t, Run{Fixture: "secret"})
	DecodeAll(t, out)
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	if len(d.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("containers = %d", len(d.Spec.Template.Spec.Containers))
	}
	for _, c := range d.Spec.Template.Spec.Containers {
		// Sorted by resource name: app-secrets, db-creds.
		if got := strings.Join(secretRefs(c), ","); got != "hello-secrets,hello-db" {
			t.Errorf("container %s envFrom secrets = %q", c.Name, got)
		}
	}
	// Existing env is untouched.
	if c := d.Spec.Template.Spec.Containers[0]; c.Name != "main" || len(c.Env) != 1 || c.Env[0].Name != "LOG_LEVEL" {
		t.Errorf("env on main = %+v", c.Env)
	}
}

func TestNoEnvFromWithoutSecret(t *testing.T) {
	out := Generate(t, Run{Fixture: "baseline"})
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	for _, c := range d.Spec.Template.Spec.Containers {
		if len(c.EnvFrom) != 0 {
			t.Errorf("container %s envFrom = %+v", c.Name, c.EnvFrom)
		}
	}
}

func TestStatefulSet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(BundleRoot(), "fixtures", "secret", "score.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sts := strings.Replace(string(raw), "  name: hello\n", "  name: hello\n  annotations:\n    k8s.score.dev/kind: StatefulSet\n", 1)
	out := Generate(t, Run{Files: map[string]string{"score.yaml": sts}})
	DecodeAll(t, out)
	s := Must[appsv1.StatefulSet](t, out, "StatefulSet", "hello")
	for _, c := range s.Spec.Template.Spec.Containers {
		if got := strings.Join(secretRefs(c), ","); got != "hello-secrets,hello-db" {
			t.Errorf("container %s envFrom secrets = %q", c.Name, got)
		}
	}
}

// Only Secret names are handled; nothing that looks like a value is written to manifests or state.
func TestNoSecretMaterial(t *testing.T) {
	out := Generate(t, Run{Fixture: "secret"})
	state, err := os.ReadFile(filepath.Join(out.Dir, ".score-k8s", "state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(out.FindAll("Secret")); n != 0 {
		t.Errorf("bundle emitted %d Secret manifests", n)
	}
	for name, b := range map[string][]byte{"manifests.yaml": out.Raw, "state.yaml": state} {
		for _, bad := range []string{"stringData", "\ndata:", "  data:"} {
			if bytes.Contains(b, []byte(bad)) {
				t.Errorf("%s contains %q", name, bad)
			}
		}
	}
}

func TestSecretRejectsBadParams(t *testing.T) {
	score := func(params string) string {
		return "apiVersion: score.dev/v1b1\nmetadata:\n  name: hello\ncontainers:\n  main:\n    image: nginx:1.27\n" +
			"resources:\n  creds:\n    type: openshift-secret\n" + params
	}
	cases := map[string]struct{ params, want string }{
		"missing name":  {"", "params.name is required"},
		"invalid name":  {"    params:\n      name: Bad_Name\n", "is not a valid Secret name"},
		"unknown param": {"    params:\n      name: ok\n      key: x\n", "unsupported param 'key'"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": score(c.params)}})
			if e := ErrContains(out, err, c.want); e != nil {
				t.Fatal(e)
			}
		})
	}
}
