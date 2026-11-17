package test

import (
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	"sigs.k8s.io/yaml"
)

func TestMinimalBuild(t *testing.T) {
	out := Generate(t, Run{Fixture: "build-minimal"})
	DecodeAll(t, out)

	// Minimal output: the workload Deployment plus exactly one ImageStream and one BuildConfig.
	if n := len(out.FindAll("ImageStream")); n != 1 {
		t.Errorf("ImageStreams = %d, want 1: %v", n, out.Kinds())
	}
	if n := len(out.FindAll("BuildConfig")); n != 1 {
		t.Errorf("BuildConfigs = %d, want 1: %v", n, out.Kinds())
	}
	if len(out.Docs) != 3 {
		t.Errorf("expected Deployment + ImageStream + BuildConfig only, got %v", out.Kinds())
	}

	is := Must[imagev1.ImageStream](t, out, "ImageStream", "hello")
	if !is.Spec.LookupPolicy.Local {
		t.Error("ImageStream lookupPolicy.local must be true")
	}
	if len(is.Spec.Tags) != 0 {
		t.Errorf("workload ImageStream should have no spec tags, got %+v", is.Spec.Tags)
	}

	bc := Must[buildv1.BuildConfig](t, out, "BuildConfig", "hello")
	s := bc.Spec
	if s.Source.Type != buildv1.BuildSourceBinary || s.Source.Binary == nil {
		t.Errorf("source = %+v, want Binary", s.Source)
	}
	if s.Strategy.Type != buildv1.DockerBuildStrategyType || s.Strategy.DockerStrategy == nil {
		t.Errorf("strategy = %+v, want Docker", s.Strategy)
	}
	if s.Output.To == nil || s.Output.To.Kind != "ImageStreamTag" || s.Output.To.Name != "hello:latest" {
		t.Errorf("output.to = %+v", s.Output.To)
	}
	if s.RunPolicy != buildv1.BuildRunPolicySerial {
		t.Errorf("runPolicy = %q", s.RunPolicy)
	}
	// Nothing optional is set.
	if s.Strategy.DockerStrategy.PullSecret != nil || s.Output.PushSecret != nil ||
		len(s.Source.Secrets) != 0 || len(s.Strategy.DockerStrategy.Volumes) != 0 {
		t.Errorf("unexpected secrets in minimal BuildConfig: %+v", s)
	}
	if len(s.Resources.Limits) != 0 || len(s.Resources.Requests) != 0 {
		t.Errorf("unexpected resources: %+v", s.Resources)
	}
	assertMapEqual(t, "BuildConfig annotations", bc.Annotations, map[string]string{"k8s.score.dev/source-workload": "hello"})
}

func TestFullBuild(t *testing.T) {
	out := Generate(t, Run{Fixture: "build-full"})
	DecodeAll(t, out)

	bc := Must[buildv1.BuildConfig](t, out, "BuildConfig", "hello")
	s := bc.Spec
	if q := s.Resources.Limits["cpu"]; q.String() != "2" {
		t.Errorf("limits.cpu = %s", q.String())
	}
	if q := s.Resources.Limits["memory"]; q.String() != "4Gi" {
		t.Errorf("limits.memory = %s", q.String())
	}
	if q := s.Resources.Requests["cpu"]; q.String() != "500m" {
		t.Errorf("requests.cpu = %s", q.String())
	}
	if p := s.Strategy.DockerStrategy.PullSecret; p == nil || p.Name != "registry-pull" {
		t.Errorf("dockerStrategy.pullSecret = %+v", p)
	}
	if p := s.Output.PushSecret; p == nil || p.Name != "registry-push" {
		t.Errorf("output.pushSecret = %+v", p)
	}
	// Build secrets are build volumes (mounted for RUN only), never source.secrets (copied into the image layer).
	if len(s.Source.Secrets) != 0 {
		t.Errorf("source.secrets must stay empty, got %+v", s.Source.Secrets)
	}
	want := []struct{ volume, secret, path string }{
		{"secret-npmrc", "npmrc", "/build-secrets/npmrc"},
		{"secret-certs", "certs", "/etc/pki/build-certs"},
	}
	vols := s.Strategy.DockerStrategy.Volumes
	if len(vols) != len(want) {
		t.Fatalf("dockerStrategy.volumes = %+v", vols)
	}
	for i, w := range want {
		v := vols[i]
		if v.Name != w.volume || v.Source.Type != buildv1.BuildVolumeSourceTypeSecret || v.Source.Secret == nil ||
			v.Source.Secret.SecretName != w.secret || len(v.Mounts) != 1 || v.Mounts[0].DestinationPath != w.path {
			t.Errorf("volumes[%d] = %+v, want %+v", i, v, w)
		}
	}

	// Each imageStreams[] entry yields one ImageStream with the right tag and from.
	streams := map[string][2]string{
		"ubi9-node":    {"20", "registry.example.com/ubi9/nodejs-20:latest"},
		"ubi9-minimal": {"latest", "registry.example.com/ubi9/ubi-minimal:9.5"},
	}
	if n := len(out.FindAll("ImageStream")); n != 1+len(streams) {
		t.Errorf("ImageStreams = %d, want %d", n, 1+len(streams))
	}
	for name, tf := range streams {
		is := Must[imagev1.ImageStream](t, out, "ImageStream", name)
		if len(is.Spec.Tags) != 1 || is.Spec.Tags[0].Name != tf[0] || is.Spec.Tags[0].From == nil ||
			is.Spec.Tags[0].From.Kind != "DockerImage" || is.Spec.Tags[0].From.Name != tf[1] {
			t.Errorf("ImageStream %s tags = %+v", name, is.Spec.Tags)
		}
	}

	// The annotations map appears on every object the provisioner emits.
	for _, d := range append(out.FindAll("ImageStream"), out.FindAll("BuildConfig")...) {
		var m struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal(d.Raw, &m); err != nil {
			t.Fatal(err)
		}
		a := m.Metadata.Annotations
		if a["team"] != "payments" || a["cost-center"] != "1234" || a["k8s.score.dev/source-workload"] != "hello" {
			t.Errorf("%s annotations = %v", d, a)
		}
	}
}

func TestBuildRejectsBadParams(t *testing.T) {
	score := func(params string) string {
		return "apiVersion: score.dev/v1b1\nmetadata:\n  name: hello\ncontainers:\n  main:\n    image: hello:latest\n" +
			"resources:\n  build:\n    type: openshift-build\n    params:\n" + params
	}
	cases := map[string]struct{ params, want string }{
		"unknown param":       {"      pullsecret: x\n", "unsupported param 'pullsecret'"},
		"secret without name": {"      secrets:\n        - destinationDir: x\n", "params.secrets[0]"},
		"destinationDir":      {"      secrets:\n        - {name: x, destinationDir: d}\n", "destinationDir is not supported"},
		"relative mountPath":  {"      secrets:\n        - {name: x, mountPath: rel}\n", "must be an absolute path"},
		"dotdot mountPath":    {"      secrets:\n        - {name: x, mountPath: /a/../b}\n", "must be an absolute path"},
		"stream without from": {"      imageStreams:\n        - name: x\n", "params.imageStreams[0] needs name and from"},
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
