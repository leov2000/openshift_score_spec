{{- /*
  40-secret-envfrom: for every workload that declares `openshift-secret` resources, append one
  envFrom.secretRef per resource (sorted by resource name) to every container and init container of its
  Deployment / StatefulSet. Secret values never pass through score-k8s.
*/ -}}
{{- range $wname, $w := .Workloads }}
{{- $secrets := list }}
{{- $res := default dict $w.resources }}
{{- range $rname := keys $res | sortAlpha }}
{{- $r := get $res $rname }}
{{- if eq (toString $r.type) "openshift-secret" }}
{{- $name := toString (dig "params" "name" "" $r) }}
{{- if or (not $name) (contains "${" $name) }}{{ fail (printf "workload '%s' resource '%s': openshift-secret params.name must be a literal Secret name" $wname $rname) }}{{ end }}
{{- $secrets = append $secrets $name }}
{{- end }}
{{- end }}
{{- if $secrets }}
{{- range $i, $m := $.Manifests }}
{{- if and (or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet")) (eq (dig "metadata" "name" "" $m) $wname) }}
{{- range $field := list "initContainers" "containers" }}
{{- range $ci, $c := (dig "spec" "template" "spec" $field list $m) }}
{{- range $s := $secrets }}
- op: set
  path: {{ $i }}.spec.template.spec.{{ $field }}.{{ $ci }}.envFrom.-1
  value:
    secretRef:
      name: {{ $s | toJson }}
  description: envFrom Secret/{{ $s }} on {{ $m.kind }}/{{ $wname }} container {{ $c.name }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
