package test

import (
	"fmt"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// TestMain refuses to run the suite against any score-k8s other than the pinned version.
func TestMain(m *testing.M) {
	pin, err := PinnedVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, "read SCORE_K8S_VERSION:", err)
		os.Exit(2)
	}
	if out, err := runCmd(".", nil, "check-version", "="+pin); err != nil {
		fmt.Fprintf(os.Stderr, "score-k8s is not the pinned version %s: %v\n%s", pin, err, out)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// The harness generates manifests from the stock sample score.yaml and decodes a Deployment and a Service.
func TestHarnessDecodesStockSample(t *testing.T) {
	out := Generate(t, Run{Sample: true, Stock: true})
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello-world")
	if got := d.Spec.Template.Spec.Containers[0].Image; got != "scorespec/sample-score-app:latest" {
		t.Errorf("image = %q", got)
	}
	s := Must[corev1.Service](t, out, "Service", "hello-world")
	if len(s.Spec.Ports) != 1 || s.Spec.Ports[0].Port != 8080 {
		t.Errorf("service ports = %+v", s.Spec.Ports)
	}
}

// check-version fails on a wrong version.
func TestCheckVersionRejectsWrongVersion(t *testing.T) {
	if out, err := runCmd(".", nil, "check-version", "=999.0.0"); err == nil {
		t.Fatalf("check-version accepted a wrong version:\n%s", out)
	}
	pin, _ := PinnedVersion()
	if out, err := runCmd(".", nil, "check-version", "="+pin); err != nil {
		t.Fatalf("check-version rejected the pinned version: %v\n%s", err, out)
	}
}
