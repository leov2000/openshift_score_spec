{{- /*
  05-workload-guard: fail generate when a workload produced no Deployment or StatefulSet.

  Stock score-k8s returns errors.Wrapf(err, ...) with err == nil in two places in convert.ConvertWorkload
  (internal/convert/workloads.go:59 and :124). Wrapf of nil is nil, so it exits 0 and silently emits nothing
  for the workload when:
    - k8s.score.dev/kind is not exactly "Deployment" or "StatefulSet" (e.g. a typo), or
    - a volume resource returns a claimSpec on a Deployment.
  An apply with --prune would then delete the running workload.
*/ -}}
{{- range $wname, $w := .Workloads }}
{{- $found := false }}
{{- range $m := $.Manifests }}
{{- if and (or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet")) (eq (dig "metadata" "name" "" $m) $wname) }}{{ $found = true }}{{ end }}
{{- end }}
{{- if not $found }}
{{- $kind := dig "metadata" "annotations" "k8s.score.dev/kind" "Deployment" $w }}
{{- fail (printf "workload '%s' produced no Deployment or StatefulSet (k8s.score.dev/kind=%q; must be Deployment or StatefulSet, and claim volumes need StatefulSet)" $wname $kind) }}
{{- end }}
{{- end }}
