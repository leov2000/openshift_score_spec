{{- /*
  30-route-annotations: copy workload metadata.annotations with the prefixes below onto each Route the workload
  owns (matched by the k8s.score.dev/source-workload annotation the route provisioner sets).

  score-k8s only puts workload annotations on the pod template, so without this patch router and cert-manager
  annotations never reach the Route.
*/ -}}
{{- $prefixes := list "cert-manager.io/" "haproxy.router.openshift.io/" }}
{{- range $i, $m := .Manifests }}
{{- if eq $m.kind "Route" }}
{{- $wname := get (dig "metadata" "annotations" dict $m) "k8s.score.dev/source-workload" }}
{{- $w := get $.Workloads $wname }}
{{- if $w }}
{{- $ann := dig "metadata" "annotations" dict $w }}
{{- range $k := keys $ann | sortAlpha }}
{{- $match := false }}
{{- range $p := $prefixes }}{{ if hasPrefix $p $k }}{{ $match = true }}{{ end }}{{ end }}
{{- if $match }}
- op: set
  path: {{ $i }}.metadata.annotations.{{ $k | replace "." "\\." }}
  value: {{ get $ann $k | toString | toJson }}
  description: copy {{ $k }} from workload {{ $wname }} to Route/{{ $m.metadata.name }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
