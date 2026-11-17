package test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestNetworkPolicySelectsWorkloadAndRoutedPort(t *testing.T) {
	// Two ports; only the routed one ("web", pod port 8080) may be opened.
	out := Generate(t, Run{Files: map[string]string{"score.yaml": `apiVersion: score.dev/v1b1
metadata:
  name: hello
containers:
  main:
    image: nginx:1.27
service:
  ports:
    web:
      port: 80
      targetPort: 8080
    metrics:
      port: 9090
resources:
  route:
    type: route
    params:
      port: web
      networkPolicy: true
`}})
	DecodeAll(t, out)
	np := Must[networkingv1.NetworkPolicy](t, out, "NetworkPolicy", "hello-route-allow-router")

	// The policy selects the workload's pods (checked against the real pod template labels).
	d := Must[appsv1.Deployment](t, out, "Deployment", "hello")
	sel := labels.SelectorFromSet(np.Spec.PodSelector.MatchLabels)
	if !sel.Matches(labels.Set(d.Spec.Template.Labels)) {
		t.Errorf("podSelector %v does not match pod labels %v", np.Spec.PodSelector.MatchLabels, d.Spec.Template.Labels)
	}
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("policyTypes = %v", np.Spec.PolicyTypes)
	}
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("ingress = %+v", np.Spec.Ingress)
	}
	rule := np.Spec.Ingress[0]
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntValue() != 8080 {
		t.Errorf("ports = %+v (want only pod port 8080)", rule.Ports)
	}
	if len(rule.From) != 1 || rule.From[0].NamespaceSelector == nil ||
		rule.From[0].NamespaceSelector.MatchLabels["network.openshift.io/policy-group"] != "ingress" {
		t.Errorf("from = %+v", rule.From)
	}
}

func TestNoNetworkPolicyByDefault(t *testing.T) {
	out := Generate(t, Run{Fixture: "route"})
	if n := len(out.FindAll("NetworkPolicy")); n != 0 {
		t.Errorf("unexpected NetworkPolicy: %v", out.Kinds())
	}
}
