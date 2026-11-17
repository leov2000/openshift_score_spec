package test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func assertRestrictedPod(t *testing.T, what string, spec corev1.PodSpec) {
	t.Helper()
	psc := spec.SecurityContext
	if psc == nil {
		t.Fatalf("%s: no pod securityContext", what)
	}
	if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
		t.Errorf("%s: runAsNonRoot not true", what)
	}
	if psc.SeccompProfile == nil || psc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("%s: seccompProfile = %+v", what, psc.SeccompProfile)
	}
	// No fixed UID/GID: the SCC must assign them from the namespace range.
	if psc.RunAsUser != nil || psc.RunAsGroup != nil || psc.FSGroup != nil {
		t.Errorf("%s: fixed UID/GID set: runAsUser=%v runAsGroup=%v fsGroup=%v", what, psc.RunAsUser, psc.RunAsGroup, psc.FSGroup)
	}
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Errorf("%s: automountServiceAccountToken not false", what)
	}
	all := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	for _, c := range all {
		sc := c.SecurityContext
		if sc == nil {
			t.Errorf("%s: container %s has no securityContext", what, c.Name)
			continue
		}
		if sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
			t.Errorf("%s: container %s capabilities = %+v", what, c.Name, sc.Capabilities)
		}
		if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
			t.Errorf("%s: container %s allowPrivilegeEscalation not false", what, c.Name)
		}
		if sc.Privileged == nil || *sc.Privileged {
			t.Errorf("%s: container %s privileged not false", what, c.Name)
		}
		if sc.RunAsUser != nil || sc.RunAsGroup != nil {
			t.Errorf("%s: container %s has fixed UID/GID", what, c.Name)
		}
	}
}

func TestDeploymentRestricted(t *testing.T) {
	out := Generate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  migrate:
    image: busybox:1.36
    before:
      main:
        ready: complete
  main:
    image: nginx:1.27
`}})
	DecodeAll(t, out)
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	if len(d.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("expected one init container, got %+v", d.Spec.Template.Spec.InitContainers)
	}
	assertRestrictedPod(t, "Deployment/hello", d.Spec.Template.Spec)
}

func TestStatefulSetRestricted(t *testing.T) {
	out := Generate(t, Run{Fixture: "statefulset"})
	s := Must[appsv1.StatefulSet](t, out, "StatefulSet", "hello")
	assertRestrictedPod(t, "StatefulSet/hello", s.Spec.Template.Spec)
}
