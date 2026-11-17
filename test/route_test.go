package test

import (
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
)

// routeScore builds a single-workload Score file with one route resource. params is the YAML body under params.
func routeScore(metaAnnotations, params string) string {
	return `apiVersion: score.dev/v1b1
metadata:
  name: hello
` + metaAnnotations + `containers:
  main:
    image: nginx:1.27
service:
  ports:
    web:
      port: 80
      targetPort: 8080
resources:
  route:
    type: route
    params:
` + params
}

func TestRouteFields(t *testing.T) {
	out := Generate(t, Run{Fixture: "route"})
	DecodeAll(t, out)
	r := Must[routev1.Route](t, out, "Route", "hello-route")
	if r.Spec.Host != "hello.apps.example.com" {
		t.Errorf("spec.host = %q", r.Spec.Host)
	}
	if r.Spec.Path != "/api" {
		t.Errorf("spec.path = %q", r.Spec.Path)
	}
	if r.Spec.To.Kind != "Service" || r.Spec.To.Name != "hello" {
		t.Errorf("spec.to = %+v", r.Spec.To)
	}
	if r.Spec.Port == nil || r.Spec.Port.TargetPort.String() != "web" {
		t.Errorf("spec.port = %+v", r.Spec.Port)
	}
	if r.Spec.TLS == nil || r.Spec.TLS.Termination != routev1.TLSTerminationEdge ||
		r.Spec.TLS.InsecureEdgeTerminationPolicy != routev1.InsecureEdgeTerminationPolicyRedirect {
		t.Errorf("spec.tls = %+v", r.Spec.TLS)
	}
	if got := r.Annotations["k8s.score.dev/source-workload"]; got != "hello" {
		t.Errorf("source-workload annotation = %q", got)
	}
}

// spec.to.name must equal the generated Service name, including when the Service is renamed.
func TestRouteTargetsGeneratedService(t *testing.T) {
	out := Generate(t, Run{Files: map[string]string{"score.yaml": routeScore(
		"  annotations:\n    k8s.score.dev/service-name: hello-svc\n",
		"      port: web\n")}})
	svc := Must[corev1.Service](t, out, "Service", "hello-svc")
	r := Must[routev1.Route](t, out, "Route", "hello-route")
	if r.Spec.To.Name != svc.Name {
		t.Errorf("spec.to.name = %q, service = %q", r.Spec.To.Name, svc.Name)
	}
	if r.Spec.Host != "" {
		t.Errorf("host should be omitted when not set, got %q", r.Spec.Host)
	}
	if r.Spec.Path != "/" {
		t.Errorf("default path = %q, want /", r.Spec.Path)
	}
}

// A numeric port param resolves to the named service port.
func TestRouteNumericPortResolvesToName(t *testing.T) {
	out := Generate(t, Run{Files: map[string]string{"score.yaml": routeScore("", "      port: 80\n")}})
	r := Must[routev1.Route](t, out, "Route", "hello-route")
	if r.Spec.Port.TargetPort.String() != "web" {
		t.Errorf("targetPort = %q, want web", r.Spec.Port.TargetPort.String())
	}
}

func TestRouteRejectsBadParams(t *testing.T) {
	cases := map[string]struct{ params, want string }{
		"unknown port":   {"      port: nope\n", "is not a service port"},
		"missing port":   {"      path: /\n", "params.port is required"},
		"trailing slash": {"      port: web\n      path: /api/\n", "must not end with /"},
		"relative path":  {"      port: web\n      path: api\n", "must start with /"},
		"bad host":       {"      port: web\n      host: Not_A_Host\n", "is not a valid hostname"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := TryGenerate(t, Run{Files: map[string]string{"score.yaml": routeScore("", c.params)}})
			if e := ErrContains(out, err, c.want); e != nil {
				t.Fatal(e)
			}
		})
	}
}

// The bundle's route provisioner wins over the stock one: no Ingress or HTTPRoute in the output.
func TestCustomRouteProvisionerWins(t *testing.T) {
	out := Generate(t, Run{Fixture: "route"})
	for _, d := range out.Docs {
		if d.Kind == "Ingress" || d.Kind == "HTTPRoute" || strings.Contains(d.APIVersion, "gateway.networking.k8s.io") {
			t.Errorf("unexpected %s %s in output", d.APIVersion, d)
		}
	}
	if len(out.FindAll("Route")) != 1 {
		t.Errorf("expected one Route, got %v", out.Kinds())
	}
}
