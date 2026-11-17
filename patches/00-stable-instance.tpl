{{- /*
  00-stable-instance: replace score-k8s's random app.kubernetes.io/instance suffix with the bare workload name.

  Stock score-k8s labels each workload "app.kubernetes.io/instance: <workload>-<10 hex chars>", where the suffix
  is generated on first generate and kept in .score-k8s/state.yaml. The same value is used in the Deployment /
  StatefulSet selector, which is immutable. In stateless CI every run starts without state, gets a new suffix,
  and the apply is rejected. Workload names are unique within a project, so the bare name is a safe instance.
*/ -}}
{{- $key := `app\.kubernetes\.io/instance` }}
{{- range $i, $m := .Manifests }}
{{- $labels := dig "metadata" "labels" dict $m }}
{{- $name := get $labels "app.kubernetes.io/name" }}
{{- $inst := get $labels "app.kubernetes.io/instance" }}
{{- if and $name (hasKey $.Workloads $name) (regexMatch (printf "^%s-[0-9a-f]{10}$" $name) $inst) }}
- op: set
  path: {{ $i }}.metadata.labels.{{ $key }}
  value: {{ $name | quote }}
  description: stable instance label on {{ $m.kind }}/{{ $name }}
{{- if eq $m.kind "Service" }}
{{- if hasKey (dig "spec" "selector" dict $m) "app.kubernetes.io/instance" }}
- op: set
  path: {{ $i }}.spec.selector.{{ $key }}
  value: {{ $name | quote }}
{{- end }}
{{- else if or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet") }}
- op: set
  path: {{ $i }}.spec.selector.matchLabels.{{ $key }}
  value: {{ $name | quote }}
- op: set
  path: {{ $i }}.spec.template.metadata.labels.{{ $key }}
  value: {{ $name | quote }}
{{- end }}
{{- end }}
{{- end }}
