package test

import (
	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/yaml"
)

func strict[T any](raw []byte) error {
	return yaml.UnmarshalStrict(raw, new(T))
}

// typedDecoders maps "apiVersion/Kind" to a strict decoder for every kind the bundle may emit.
var typedDecoders = map[string]func([]byte) error{
	"v1/Service":                             strict[corev1.Service],
	"v1/ConfigMap":                           strict[corev1.ConfigMap],
	"v1/Secret":                              strict[corev1.Secret],
	"v1/PersistentVolumeClaim":               strict[corev1.PersistentVolumeClaim],
	"apps/v1/Deployment":                     strict[appsv1.Deployment],
	"apps/v1/StatefulSet":                    strict[appsv1.StatefulSet],
	"route.openshift.io/v1/Route":            strict[routev1.Route],
	"build.openshift.io/v1/BuildConfig":      strict[buildv1.BuildConfig],
	"image.openshift.io/v1/ImageStream":      strict[imagev1.ImageStream],
	"autoscaling/v2/HorizontalPodAutoscaler": strict[autoscalingv2.HorizontalPodAutoscaler],
	"policy/v1/PodDisruptionBudget":          strict[policyv1.PodDisruptionBudget],
	"networking.k8s.io/v1/NetworkPolicy":     strict[networkingv1.NetworkPolicy],
}
