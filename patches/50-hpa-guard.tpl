{{- /*
  50-hpa-guard: a workload with an `hpa` resource must set a CPU request on every container (and every
  restartPolicy: Always sidecar), or the HPA reports <unknown> and never scales.
*/ -}}
{{- range $wname, $w := .Workloads }}
{{- $hasHpa := false }}
{{- range $rname, $r := (default dict $w.resources) }}{{ if eq (toString $r.type) "hpa" }}{{ $hasHpa = true }}{{ end }}{{ end }}
{{- if $hasHpa }}
{{- range $m := $.Manifests }}
{{- if and (or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet")) (eq (dig "metadata" "name" "" $m) $wname) }}
{{- $podSpec := dig "spec" "template" "spec" dict $m }}
{{- $checked := (default list $podSpec.containers) }}
{{- range $c := (default list $podSpec.initContainers) }}{{ if eq (toString $c.restartPolicy) "Always" }}{{ $checked = append $checked $c }}{{ end }}{{ end }}
{{- range $c := $checked }}
{{- if not (dig "resources" "requests" "cpu" "" $c) }}{{ fail (printf "workload '%s' declares an hpa resource but container '%s' has no CPU request (set containers.%s.resources.requests.cpu)" $wname $c.name $c.name) }}{{ end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
