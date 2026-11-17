{{- /*
  20-image-trigger: for every workload that declares an `openshift-build` resource, add the
  image.openshift.io/triggers annotation to its Deployment / StatefulSet so each new build of
  <workload>:latest rolls the workload without a re-apply.

  Only containers whose image is exactly "<workload>:latest" are wired. If the workload declares a build but no
  container uses that image, generate fails: the build would never be deployed.
*/ -}}
{{- range $wname, $w := .Workloads }}
{{- $hasBuild := false }}
{{- range $rname, $r := (default dict $w.resources) }}{{ if eq (toString $r.type) "openshift-build" }}{{ $hasBuild = true }}{{ end }}{{ end }}
{{- if $hasBuild }}
{{- $tag := printf "%s:latest" $wname }}
{{- $found := false }}
{{- range $i, $m := $.Manifests }}
{{- if and (or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet")) (eq (dig "metadata" "name" "" $m) $wname) }}
{{- $found = true }}
{{- $triggers := list }}
{{- range $field := list "containers" "initContainers" }}
{{- range $c := (dig "spec" "template" "spec" $field list $m) }}
{{- if eq $c.image $tag }}
{{- $triggers = append $triggers (dict "from" (dict "kind" "ImageStreamTag" "name" $tag) "fieldPath" (printf "spec.template.spec.%s[?(@.name==\"%s\")].image" $field $c.name) "paused" false) }}
{{- end }}
{{- end }}
{{- end }}
{{- if not $triggers }}{{ fail (printf "workload '%s' declares an openshift-build resource but no container uses image '%s'" $wname $tag) }}{{ end }}
- op: set
  path: {{ $i }}.metadata.annotations.image\.openshift\.io/triggers
  value: {{ toJson $triggers | squote }}
  description: image trigger on {{ $m.kind }}/{{ $wname }}
{{- end }}
{{- end }}
{{- if not $found }}{{ fail (printf "workload '%s' declares an openshift-build resource but no Deployment or StatefulSet named '%s' was generated" $wname $wname) }}{{ end }}
{{- end }}
{{- end }}
