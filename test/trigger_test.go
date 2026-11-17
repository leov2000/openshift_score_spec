package test

import (
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

const triggerAnnotation = "image.openshift.io/triggers"

type imageTrigger struct {
	From struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace,omitempty"`
	} `json:"from"`
	FieldPath string `json:"fieldPath"`
	Paused    bool   `json:"paused"`
}

func decodeTriggers(t *testing.T, raw string) []imageTrigger {
	t.Helper()
	var out []imageTrigger
	dec := json.NewDecoder(stringsReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("trigger annotation is not valid JSON: %v\n%s", err, raw)
	}
	return out
}

func TestTriggerPresentWithBuild(t *testing.T) {
	out := Generate(t, Run{Fixture: "build-minimal"})
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	raw, ok := d.Annotations[triggerAnnotation]
	if !ok {
		t.Fatalf("missing %s; annotations = %v", triggerAnnotation, d.Annotations)
	}
	tr := decodeTriggers(t, raw)
	if len(tr) != 1 {
		t.Fatalf("triggers = %+v", tr)
	}
	if tr[0].From.Kind != "ImageStreamTag" || tr[0].From.Name != "hello:latest" {
		t.Errorf("from = %+v", tr[0].From)
	}
	if tr[0].FieldPath != `spec.template.spec.containers[?(@.name=="main")].image` {
		t.Errorf("fieldPath = %s", tr[0].FieldPath)
	}
	if tr[0].Paused {
		t.Error("trigger must not be paused")
	}
}

func TestTriggerAbsentWithoutBuild(t *testing.T) {
	for _, fx := range []string{"baseline", "route"} {
		out := Generate(t, Run{Fixture: fx})
		d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
		if _, ok := d.Annotations[triggerAnnotation]; ok {
			t.Errorf("%s: unexpected %s", fx, triggerAnnotation)
		}
	}
}

// Only containers using <workload>:latest are wired; others are left alone.
func TestTriggerOnlyForBuiltContainers(t *testing.T) {
	out := Generate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  main:
    image: hello:latest
  proxy:
    image: envoyproxy/envoy:v1.31
resources:
  build:
    type: openshift-build
`}})
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	tr := decodeTriggers(t, d.Annotations[triggerAnnotation])
	if len(tr) != 1 || tr[0].FieldPath != `spec.template.spec.containers[?(@.name=="main")].image` {
		t.Errorf("triggers = %+v", tr)
	}
}

func TestBuildWithoutMatchingImageFails(t *testing.T) {
	out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  main:
    image: nginx:1.27
resources:
  build:
    type: openshift-build
`}})
	if e := ErrContains(out, err, "no container uses image 'hello:latest'"); e != nil {
		t.Fatal(e)
	}
}
