package controllers

import (
	"context"
	"math"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	phpav1 "myw.domain/predictivehybridpodautoscaler/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Interface ScaleExecutor executes the given scale decision.
type ScaleExecutor interface {
	// Scale with desired replicas and container resource requirements.
	// Return true if scaled and false if not.
	// Also return error.
	scaleWithDesiredStrategy(int32, map[string]corev1.ResourceRequirements) (ifscale bool, err error)
}

// HorizontalScaleExecutor only executes horizontal scale decision.
type HorizontalScaleExecutor struct {
	ctx context.Context
	client.Client
	deployment                           *appsv1.Deployment
	currentReplicas                      int32
	scaleDownStabilizationWindowDuration *time.Duration
	lastScaleTime                        *metav1.Time
}

// Horizontally scale pods.
func (h HorizontalScaleExecutor) scaleWithDesiredStrategy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	if desiredReplicas == h.currentReplicas {
		return false, nil
	} else if desiredReplicas > h.currentReplicas {
		h.deployment.Spec.Replicas = &desiredReplicas
		if err := h.Update(h.ctx, h.deployment); err != nil {
			return true, err
		}
		return true, nil
	} else {
		ifScale := false
		if h.lastScaleTime != nil {
			ifScale = time.Now().After(h.lastScaleTime.Time.Add(*h.scaleDownStabilizationWindowDuration))
		} else {
			ifScale = true
		}
		if ifScale {
			h.deployment.Spec.Replicas = &desiredReplicas
			if err := h.Update(h.ctx, h.deployment); err != nil {
				return true, err
			}
		}
		return ifScale, nil
	}
}

// VerticalScaleExecutor only executes vertical scale decision.
type VerticalScaleExecutor struct {
	ctx context.Context
	client.Client
	pods                                 []corev1.Pod
	deployment                           *appsv1.Deployment
	currentReplicas                      int32
	scaleDownStabilizationWindowDuration *time.Duration
	lastScaleTime                        *metav1.Time
	verticalScalePolicy                  phpav1.VerticalScalePolicy
}

// Vertically scale pods with desired strategy.
func (v VerticalScaleExecutor) scaleWithDesiredStrategy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	switch v.verticalScalePolicy.Type {
	case phpav1.VerticalScalePolicyTypeNormal:
		{
			return v.scaleWithNormalPolicy(desiredReplicas, desiredResourceRequirements)
		}
	case phpav1.VerticalScalePolicyTypeAvailable:
		{
			return v.scaleWithAvailablePolicy(desiredReplicas, desiredResourceRequirements)
		}
	case phpav1.VerticalScalePolicyTypeSafe:
		{
			return v.scaleWithSafePolicy(desiredReplicas, desiredResourceRequirements)
		}
	}
	return false, nil
}

// Vertically scale pods with the normal strategy.
// Replace the old pods by new ones using rolling update.
func (v VerticalScaleExecutor) scaleWithNormalPolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	log := log.FromContext(v.ctx)
	replicas := *v.deployment.Spec.Replicas
	evictionTolerance := float64(v.verticalScalePolicy.EvictionTolerance) / 100
	scaleablePodCount := int32(math.Ceil(math.Max(evictionTolerance*float64(replicas), 1) + 0.5))

	scaledPodCount := int32(0)
	// Find the pods to be scaled.
	for _, pod := range v.pods {
		ifScale := false
		containers := pod.Spec.Containers
		for _, container := range containers {
			if !requirementIsSameAs(container.Resources, desiredResourceRequirements[container.Name]) {
				ifScale = true
				break
			}
		}
		if ifScale && scaledPodCount < scaleablePodCount {
			scaledPodCount++
			if err := v.Delete(v.ctx, &pod); err != nil {
				log.Error(err, "failed to delete pod %v\n", err)
			}
		}
	}
	return true, nil
}

func (v VerticalScaleExecutor) scaleWithAvailablePolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	return false, nil
}

func (v VerticalScaleExecutor) scaleWithSafePolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	return false, nil
}

// Check if the current ResourceRequirements is the same as the desired one.
func requirementIsSameAs(current corev1.ResourceRequirements, desired corev1.ResourceRequirements) bool {
	if desired.Requests != nil {
		if current.Requests == nil {
			return false
		}
		if desired.Requests.Cpu() != current.Requests.Cpu() || desired.Requests.Memory() != current.Requests.Memory() {
			return false
		}
	}
	if desired.Limits != nil {
		if current.Limits == nil {
			return false
		}
		if desired.Limits.Cpu() != current.Limits.Cpu() || desired.Limits.Memory() != current.Limits.Memory() {
			return false
		}
	}
	return true
}

func quantityIsSameAs(q1 resource.Quantity, q2 resource.Quantity) bool {
	return q1.MilliValue() == q2.MilliValue()
}
