package test

import (
	"bytes"
	"regexp"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// These tests record what stock score-k8s emits. The provisioners and patches rely on these facts, so a
// score-k8s upgrade that changes any of them must fail here first.

// Deployment metadata.name equals the Score workload name; the Service also uses the workload name.
func TestDeploymentNamedAfterWorkload(t *testing.T) {
	out := Generate(t, Run{Fixture: "baseline", Stock: true})
	if got := out.Kinds(); len(got) != 2 {
		t.Fatalf("expected exactly a Service and a Deployment, got %v", got)
	}
	Must[appsv1.Deployment](t, out, "Deployment", "hello")
	Must[corev1.Service](t, out, "Service", "hello")
}

// Recorded: workload metadata.annotations land on the pod template only. The Deployment and Service carry
// only k8s.score.dev/workload-name. Anything that needs workload annotations elsewhere (Route) must copy them.
func TestWorkloadAnnotationsOnlyOnPodTemplate(t *testing.T) {
	out := Generate(t, Run{Fixture: "baseline", Stock: true})
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	s := Must[corev1.Service](t, out, "Service", "hello")

	want := map[string]string{"k8s.score.dev/workload-name": "hello"}
	assertMapEqual(t, "Deployment.metadata.annotations", d.Annotations, want)
	assertMapEqual(t, "Service.metadata.annotations", s.Annotations, want)
	assertMapEqual(t, "pod template annotations", d.Spec.Template.Annotations, map[string]string{
		"example.com/team":                    "payments",
		"haproxy.router.openshift.io/timeout": "60s",
		"k8s.score.dev/workload-name":         "hello",
	})
}

var instanceLabel = regexp.MustCompile(`app\.kubernetes\.io/instance: hello-[0-9a-f]{10}`)

// Recorded: stock output is NOT byte-identical across fresh directories. The only state-dependent value for
// a plain workload is the random suffix on app.kubernetes.io/instance (WorkloadExtras.InstanceSuffix in
// .score-k8s/state.yaml), which is also used in the Deployment's immutable selector. Resource provisioners
// can add more (.Guid, randAlphaNum). The bundle's 00-stable-instance.tpl patch removes the suffix.
func TestStockOutputDependsOnState(t *testing.T) {
	a := Generate(t, Run{Fixture: "baseline", Stock: true})
	b := Generate(t, Run{Fixture: "baseline", Stock: true})
	if bytes.Equal(a.Raw, b.Raw) {
		t.Fatalf("stock output was byte-identical across fresh dirs; the instance-suffix assumption is stale")
	}
	na := instanceLabel.ReplaceAll(a.Raw, []byte("app.kubernetes.io/instance: X"))
	nb := instanceLabel.ReplaceAll(b.Raw, []byte("app.kubernetes.io/instance: X"))
	if !bytes.Equal(na, nb) {
		t.Fatalf("outputs differ in more than the instance label:\n--- a\n%s\n--- b\n%s", na, nb)
	}
}

// With the bundle installed, two fresh directories produce byte-identical output.
func TestBundleOutputIsDeterministic(t *testing.T) {
	a := Generate(t, Run{Fixture: "baseline"})
	b := Generate(t, Run{Fixture: "baseline"})
	if !bytes.Equal(a.Raw, b.Raw) {
		t.Fatalf("bundle output differs across fresh dirs:\n--- a\n%s\n--- b\n%s", a.Raw, b.Raw)
	}
	d := Must[appsv1.Deployment](t, a, "Deployment", "hello")
	if got := d.Spec.Selector.MatchLabels["app.kubernetes.io/instance"]; got != "hello" {
		t.Errorf("selector instance = %q, want hello", got)
	}
	s := Must[corev1.Service](t, a, "Service", "hello")
	if got := s.Spec.Selector["app.kubernetes.io/instance"]; got != "hello" {
		t.Errorf("service selector instance = %q, want hello", got)
	}
}

// Recorded: stock score-k8s also emits workloads in Go map order, so with two or more workloads the manifest
// order varies between runs. With the bundle (99-sort.tpl) repeated runs are byte-identical.
func TestMultiWorkloadOutputIsDeterministic(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"alpha", "bravo", "charlie", "delta"} {
		files[n+".score.yaml"] = "apiVersion: score.dev/v1b1\nmetadata:\n  name: " + n +
			"\ncontainers:\n  main:\n    image: nginx:1.27\nservice:\n  ports:\n    web:\n      port: 8080\n"
	}
	first := Generate(t, Run{Files: files})
	for i := 0; i < 5; i++ {
		again := Generate(t, Run{Files: files})
		if !bytes.Equal(first.Raw, again.Raw) {
			t.Fatalf("run %d differs:\n%v\nvs\n%v", i+2, first.Kinds(), again.Kinds())
		}
	}
}

// Liveness and readiness probes from the Score file appear on the container.
func TestProbesOnContainer(t *testing.T) {
	out := Generate(t, Run{Fixture: "baseline", Stock: true})
	c := Must[appsv1.Deployment](t, out, "Deployment", "hello").Spec.Template.Spec.Containers[0]
	if c.LivenessProbe == nil || c.LivenessProbe.HTTPGet == nil || c.LivenessProbe.HTTPGet.Path != "/healthz" || c.LivenessProbe.HTTPGet.Port.IntValue() != 8080 {
		t.Errorf("liveness probe = %+v", c.LivenessProbe)
	}
	if c.ReadinessProbe == nil || c.ReadinessProbe.HTTPGet == nil || c.ReadinessProbe.HTTPGet.Path != "/ready" {
		t.Errorf("readiness probe = %+v", c.ReadinessProbe)
	}
}

func assertMapEqual(t testing.TB, what string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}
