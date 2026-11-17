{{- /*
  60-security-context: based on score-spec/community-patchers score-k8s/unprivileged.tpl, adapted for OpenShift:
    - runAsUser, runAsGroup and fsGroup removed, so the restricted-v2 SCC assigns a UID/GID from the namespace range.
    - applied to StatefulSets as well as Deployments, and to init containers as well as containers.
*/ -}}
{{- range $i, $m := .Manifests }}
{{- if or (eq $m.kind "Deployment") (eq $m.kind "StatefulSet") }}
- op: set
  path: {{ $i }}.spec.template.spec.automountServiceAccountToken
  value: false
- op: set
  path: {{ $i }}.spec.template.spec.securityContext
  value:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  description: restricted pod securityContext on {{ $m.kind }}/{{ $m.metadata.name }}
{{- range $field := list "initContainers" "containers" }}
{{- range $ci, $_ := (dig "spec" "template" "spec" $field list $m) }}
- op: set
  path: {{ $i }}.spec.template.spec.{{ $field }}.{{ $ci }}.securityContext
  value:
    allowPrivilegeEscalation: false
    privileged: false
    readOnlyRootFilesystem: true
    capabilities:
      drop:
        - ALL
{{- end }}
{{- end }}
{{- end }}
{{- end }}
