{{- /*
  99-sort: put manifests in a stable order (Namespace first, then by kind, then name). Must run last.

  Stock score-k8s appends workload manifests while ranging over a Go map (`for workloadName := range
  state.Workloads` in generate.go), so with two or more workloads the order changes from run to run. That breaks
  golden files and makes CI diffs noisy. Patches can only set paths, so each out-of-place index is overwritten
  with the manifest that belongs there.
*/ -}}
{{- $keys := list }}
{{- range $i, $m := .Manifests }}
{{- $prio := ternary "0" "1" (eq (toString $m.kind) "Namespace") }}
{{- $keys = append $keys (printf "%s\x00%s\x00%s\x00%s\x00%06d" $prio (toString $m.kind) (dig "metadata" "namespace" "" $m) (dig "metadata" "name" "" $m) $i) }}
{{- end }}
{{- range $j, $k := sortAlpha $keys }}
{{- $idx := atoi (last (splitList "\x00" $k)) }}
{{- if ne $idx $j }}
- op: set
  path: "{{ $j }}"
  value: {{ index $.Manifests $idx | toJson }}
{{- end }}
{{- end }}
