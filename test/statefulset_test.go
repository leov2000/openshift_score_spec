package test

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestStatefulSetWithClaimTemplate(t *testing.T) {
	out := Generate(t, Run{Fixture: "statefulset"})
	DecodeAll(t, out)
	if n := len(out.FindAll("Deployment")); n != 0 {
		t.Errorf("unexpected Deployment: %v", out.Kinds())
	}
	if n := len(out.FindAll("PersistentVolumeClaim")); n != 0 {
		t.Errorf("StatefulSet volumes must be claim templates, not standalone PVCs: %v", out.Kinds())
	}
	s := Must[appsv1.StatefulSet](t, out, "StatefulSet", "hello")

	// Recorded: the volume appears as a volumeClaimTemplate named after the source hash, mounted by name,
	// and NOT in pod volumes.
	if len(s.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("volumeClaimTemplates = %+v", s.Spec.VolumeClaimTemplates)
	}
	vct := s.Spec.VolumeClaimTemplates[0]
	if !strings.HasPrefix(vct.Name, "vol-") {
		t.Errorf("claim template name = %q", vct.Name)
	}
	if q := vct.Spec.Resources.Requests[corev1.ResourceStorage]; q.String() != "5Gi" {
		t.Errorf("storage = %s", q.String())
	}
	if vct.Spec.StorageClassName == nil || *vct.Spec.StorageClassName != "fast-ssd" {
		t.Errorf("storageClassName = %v", vct.Spec.StorageClassName)
	}
	if len(vct.Spec.AccessModes) != 1 || vct.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("accessModes = %v", vct.Spec.AccessModes)
	}
	for _, v := range s.Spec.Template.Spec.Volumes {
		if v.Name == vct.Name {
			t.Errorf("claim %s also listed as a pod volume", v.Name)
		}
	}
	c := s.Spec.Template.Spec.Containers[0]
	if len(c.VolumeMounts) != 1 || c.VolumeMounts[0].Name != vct.Name || c.VolumeMounts[0].MountPath != "/data" {
		t.Errorf("volumeMounts = %+v", c.VolumeMounts)
	}

	// Steps 4 and 6 still apply to the StatefulSet.
	if _, ok := s.Annotations[triggerAnnotation]; !ok {
		t.Errorf("StatefulSet missing %s", triggerAnnotation)
	}
	if got := strings.Join(secretRefs(c), ","); got != "hello-secrets" {
		t.Errorf("envFrom secrets = %q", got)
	}
}

// On a Deployment the volume is one shared PVC, instead of score-k8s silently dropping the workload.
func TestDeploymentGetsSharedPVC(t *testing.T) {
	out := Generate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  main:
    image: nginx:1.27
    volumes:
      /data:
        source: ${resources.data}
resources:
  data:
    type: volume
`}})
	DecodeAll(t, out)
	pvc := Must[corev1.PersistentVolumeClaim](t, out, "PersistentVolumeClaim", "hello-data")
	if q := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; q.String() != "1Gi" {
		t.Errorf("default size = %s", q.String())
	}
	if pvc.Spec.StorageClassName != nil {
		t.Errorf("storageClassName should be omitted by default, got %q", *pvc.Spec.StorageClassName)
	}
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	vols := d.Spec.Template.Spec.Volumes
	if len(vols) != 1 || vols[0].PersistentVolumeClaim == nil || vols[0].PersistentVolumeClaim.ClaimName != "hello-data" {
		t.Errorf("pod volumes = %+v", vols)
	}
}

// Guard for the score-k8s silent-drop bug: an unsupported kind fails instead of emitting nothing.
func TestUnsupportedKindFails(t *testing.T) {
	out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
  annotations:
    k8s.score.dev/kind: Statefulset
containers:
  main:
    image: nginx:1.27
`}})
	if e := ErrContains(out, err, "produced no Deployment or StatefulSet"); e != nil {
		t.Fatal(e)
	}
}

// Recorded: stock score-k8s exits 0 with no workload for an unsupported kind. If this starts failing,
// score-k8s fixed the bug and 05-workload-guard.tpl can be dropped.
func TestStockSilentlyDropsUnsupportedKind(t *testing.T) {
	out := Generate(t, Run{Stock: true, Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
  annotations:
    k8s.score.dev/kind: Statefulset
containers:
  main:
    image: nginx:1.27
`}})
	if len(out.Docs) != 0 {
		t.Fatalf("stock score-k8s now emits %v for an unsupported kind; revisit 05-workload-guard.tpl", out.Kinds())
	}
}

func TestVolumeRejectsBadParams(t *testing.T) {
	score := func(params string) string {
		return "apiVersion: score.dev/v1b1\nmetadata:\n  name: hello\n  annotations:\n    k8s.score.dev/kind: StatefulSet\n" +
			"containers:\n  main:\n    image: nginx:1.27\n    volumes:\n      /data:\n        source: ${resources.data}\n" +
			"resources:\n  data:\n    type: volume\n    params:\n" + params
	}
	cases := map[string]struct{ params, want string }{
		"bad size":      {"      size: lots\n", "must be a quantity"},
		"bad mode":      {"      accessMode: RWO\n", "is not a valid access mode"},
		"unknown param": {"      class: x\n", "unsupported param 'class'"},
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
