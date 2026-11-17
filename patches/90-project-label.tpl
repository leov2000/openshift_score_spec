{{- /*
  90-project-label: when SCORE_OCP_PROJECT is set (bin/score-ocp sets it), label every generated object with
  app.kubernetes.io/part-of=<project>. `oc apply --prune -l app.kubernetes.io/part-of=<project>` then deletes
  objects whose resource or workload was removed from the Score files. No-op when the variable is unset.
*/ -}}
{{- $p := env "SCORE_OCP_PROJECT" }}
{{- if $p }}
{{- if or (gt (len $p) 63) (not (regexMatch `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` $p)) }}{{ fail (printf "SCORE_OCP_PROJECT '%s' must be a lowercase DNS label (max 63 chars)" $p) }}{{ end }}
{{- range $i, $m := .Manifests }}
- op: set
  path: {{ $i }}.metadata.labels.app\.kubernetes\.io/part-of
  value: {{ $p | toJson }}
{{- end }}
{{- end }}
