package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	policyv1 "k8s.io/api/policy/v1"
)

func hpaFixture(t *testing.T, edit func(string) string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(BundleRoot(), "fixtures", "hpa", "score.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return edit(string(raw))
}

func TestHPATargetsDeployment(t *testing.T) {
	out := Generate(t, Run{Fixture: "hpa"})
	DecodeAll(t, out)
	h := Must[autoscalingv2.HorizontalPodAutoscaler](t, out, "HorizontalPodAutoscaler", "hello")
	ref := h.Spec.ScaleTargetRef
	if ref.APIVersion != "apps/v1" || ref.Kind != "Deployment" || ref.Name != "hello" {
		t.Errorf("scaleTargetRef = %+v", ref)
	}
	out.Find(t, ref.Kind, ref.Name) // target object exists in the output
	if h.Spec.MinReplicas == nil || *h.Spec.MinReplicas != 2 || h.Spec.MaxReplicas != 5 {
		t.Errorf("replicas = %v..%d", h.Spec.MinReplicas, h.Spec.MaxReplicas)
	}
	if len(h.Spec.Metrics) != 1 || h.Spec.Metrics[0].Resource == nil || h.Spec.Metrics[0].Resource.Name != "cpu" ||
		h.Spec.Metrics[0].Resource.Target.AverageUtilization == nil || *h.Spec.Metrics[0].Resource.Target.AverageUtilization != 75 {
		t.Errorf("metrics = %+v", h.Spec.Metrics)
	}
}

func TestHPATargetsStatefulSet(t *testing.T) {
	score := hpaFixture(t, func(s string) string {
		return strings.Replace(s, "  name: hello\n", "  name: hello\n  annotations:\n    k8s.score.dev/kind: StatefulSet\n", 1)
	})
	out := Generate(t, Run{Files: map[string]string{"score.yaml": score}})
	DecodeAll(t, out)
	h := Must[autoscalingv2.HorizontalPodAutoscaler](t, out, "HorizontalPodAutoscaler", "hello")
	if h.Spec.ScaleTargetRef.Kind != "StatefulSet" {
		t.Errorf("scaleTargetRef.kind = %q", h.Spec.ScaleTargetRef.Kind)
	}
	out.Find(t, "StatefulSet", h.Spec.ScaleTargetRef.Name)
}

func TestPDBOnlyWhenMinReplicasAtLeastTwo(t *testing.T) {
	out := Generate(t, Run{Fixture: "hpa"})
	pdb := Must[policyv1.PodDisruptionBudget](t, out, "PodDisruptionBudget", "hello")
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 1 {
		t.Errorf("maxUnavailable = %v", pdb.Spec.MaxUnavailable)
	}
	if pdb.Spec.Selector.MatchLabels["app.kubernetes.io/name"] != "hello" {
		t.Errorf("selector = %+v", pdb.Spec.Selector)
	}

	for _, min := range []string{"", "      minReplicas: 1\n"} {
		score := hpaFixture(t, func(s string) string {
			return strings.Replace(s, "      minReplicas: 2\n", min, 1)
		})
		out := Generate(t, Run{Files: map[string]string{"score.yaml": score}})
		if n := len(out.FindAll("PodDisruptionBudget")); n != 0 {
			t.Errorf("minReplicas=%q: unexpected PDB", strings.TrimSpace(min))
		}
		if n := len(out.FindAll("HorizontalPodAutoscaler")); n != 1 {
			t.Errorf("minReplicas=%q: HPAs = %d", strings.TrimSpace(min), n)
		}
	}
}

func TestHPARejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		edit func(string) string
		want string
	}{
		"missing maxReplicas": {
			func(s string) string { return strings.Replace(s, "      maxReplicas: 5\n", "", 1) },
			"params.maxReplicas is required",
		},
		"min greater than max": {
			func(s string) string { return strings.Replace(s, "minReplicas: 2", "minReplicas: 6", 1) },
			"must not exceed params.maxReplicas",
		},
		"non-integer": {
			func(s string) string { return strings.Replace(s, "maxReplicas: 5", "maxReplicas: lots", 1) },
			"params.maxReplicas must be a positive integer",
		},
		"no cpu request": {
			func(s string) string { return strings.Replace(s, "        cpu: 100m\n", "", 1) },
			"container 'main' has no CPU request",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": hpaFixture(t, c.edit)}})
			if e := ErrContains(out, err, c.want); e != nil {
				t.Fatal(e)
			}
		})
	}
}
