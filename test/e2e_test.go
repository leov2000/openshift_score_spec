package test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

var update = flag.Bool("update", false, "rewrite golden files")

func generateFull(t *testing.T) *Output {
	return Generate(t, Run{
		Fixture:    "full",
		ScoreFiles: fullScoreFiles,
		Namespace:  "shop-dev",
		Env:        []string{"SCORE_OCP_PROJECT=shop"},
	})
}

func TestFullFixtureTypedAndWired(t *testing.T) {
	out := generateFull(t)
	DecodeAll(t, out)

	// Stable order from 99-sort.tpl: by kind, then name.
	want := []string{
		"BuildConfig/web", "Deployment/web", "HorizontalPodAutoscaler/web", "ImageStream/ubi9-node",
		"ImageStream/web", "PodDisruptionBudget/web", "Route/web-route", "Service/cache",
		"Service/cache-headless-svc", "Service/web", "StatefulSet/cache",
	}
	if got := strings.Join(out.Kinds(), " "); got != strings.Join(want, " ") {
		t.Errorf("objects:\n got %s\nwant %s", got, strings.Join(want, " "))
	}

	web := Must[appsv1.Deployment](t, out, "Deployment", "web")
	cache := Must[appsv1.StatefulSet](t, out, "StatefulSet", "cache")
	webSvc := Must[corev1.Service](t, out, "Service", "web")
	route := Must[routev1.Route](t, out, "Route", "web-route")
	hpa := Must[autoscalingv2.HorizontalPodAutoscaler](t, out, "HorizontalPodAutoscaler", "web")
	Must[policyv1.PodDisruptionBudget](t, out, "PodDisruptionBudget", "web")
	Must[buildv1.BuildConfig](t, out, "BuildConfig", "web")
	is := Must[imagev1.ImageStream](t, out, "ImageStream", "web")

	// build -> image -> trigger
	if !is.Spec.LookupPolicy.Local {
		t.Error("web ImageStream local lookup off")
	}
	if web.Spec.Template.Spec.Containers[0].Image != "web:latest" || web.Annotations[triggerAnnotation] == "" {
		t.Error("web Deployment not wired to its ImageStream")
	}
	if _, ok := cache.Annotations[triggerAnnotation]; ok {
		t.Error("cache has no build but got a trigger")
	}
	// route -> service, annotations copied
	if route.Spec.To.Name != webSvc.Name || route.Spec.Port.TargetPort.String() != "http" {
		t.Errorf("route target = %s:%s", route.Spec.To.Name, route.Spec.Port.TargetPort.String())
	}
	if route.Annotations["haproxy.router.openshift.io/timeout"] != "120s" || route.Annotations["cert-manager.io/issuer-name"] != "corp-ca" {
		t.Errorf("route annotations = %v", route.Annotations)
	}
	// secrets
	if got := strings.Join(secretRefs(web.Spec.Template.Spec.Containers[0]), ","); got != "web-secrets" {
		t.Errorf("web envFrom = %q", got)
	}
	if got := strings.Join(secretRefs(cache.Spec.Template.Spec.Containers[0]), ","); got != "cache-secrets" {
		t.Errorf("cache envFrom = %q", got)
	}
	// service-port output resolved into env
	env := map[string]string{}
	for _, e := range web.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if env["CACHE_HOST"] != "cache" || env["CACHE_PORT"] != "6379" {
		t.Errorf("web env = %v", env)
	}
	// hpa -> deployment
	if hpa.Spec.ScaleTargetRef.Kind != "Deployment" || hpa.Spec.ScaleTargetRef.Name != "web" {
		t.Errorf("hpa target = %+v", hpa.Spec.ScaleTargetRef)
	}
	// statefulset storage
	if len(cache.Spec.VolumeClaimTemplates) != 1 {
		t.Errorf("cache claim templates = %d", len(cache.Spec.VolumeClaimTemplates))
	}
	// security context everywhere
	assertRestrictedPod(t, "Deployment/web", web.Spec.Template.Spec)
	assertRestrictedPod(t, "StatefulSet/cache", cache.Spec.Template.Spec)
	// namespace on everything
	if c := bytes.Count(out.Raw, []byte("\n  namespace: shop-dev\n")); c != len(out.Docs) {
		t.Errorf("namespace set on %d of %d objects", c, len(out.Docs))
	}
}

// Golden file: guards against output drift on score-k8s or bundle upgrades. Regenerate with
//
//	go test ./... -run TestGolden -update
func TestGolden(t *testing.T) {
	out := generateFull(t)
	golden := filepath.Join(BundleRoot(), "fixtures", "full", "manifests.golden.yaml")
	if *update {
		if err := os.WriteFile(golden, out.Raw, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(out.Raw, want) {
		t.Fatalf("output drifted from %s; review the diff and re-run with -update if intended.\n--- got\n%s", golden, out.Raw)
	}
}
