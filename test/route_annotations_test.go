package test

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
)

func TestRouteAnnotationsCopied(t *testing.T) {
	out := Generate(t, Run{Fixture: "route-annotations"})
	DecodeAll(t, out)

	alpha := Must[routev1.Route](t, out, "Route", "alpha-route")
	// Matching annotations arrive with keys intact (dots not mangled by the sjson path escaping).
	assertMapEqual(t, "alpha Route annotations", alpha.Annotations, map[string]string{
		"haproxy.router.openshift.io/timeout":      "120s",
		"haproxy.router.openshift.io/ip_whitelist": "10.0.0.0/8 192.168.0.0/16",
		"cert-manager.io/issuer-kind":              "ClusterIssuer",
		"cert-manager.io/issuer-name":              "corp-ca",
		"k8s.score.dev/source-workload":            "alpha",
		"k8s.score.dev/resource-uid":               "route.default#alpha.route",
	})

	// Two workloads in one project each get only their own annotations.
	beta := Must[routev1.Route](t, out, "Route", "beta-route")
	assertMapEqual(t, "beta Route annotations", beta.Annotations, map[string]string{
		"haproxy.router.openshift.io/timeout": "5s",
		"k8s.score.dev/source-workload":       "beta",
		"k8s.score.dev/resource-uid":          "route.default#beta.route",
	})
}

// Without matching workload annotations the Route keeps only the provisioner's own annotations.
func TestNoMatchingAnnotations(t *testing.T) {
	out := Generate(t, Run{Fixture: "route"})
	r := Must[routev1.Route](t, out, "Route", "hello-route")
	assertMapEqual(t, "Route annotations", r.Annotations, map[string]string{
		"k8s.score.dev/source-workload": "hello",
		"k8s.score.dev/resource-uid":    "route.default#hello.route",
	})
}
