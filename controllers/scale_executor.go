package controllers

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	autoscalingv1 "myw.domain/autoscaling/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ScaleExecutor interface {
	// Scale with desired replicas and container resource requirements.
	// Return true if scaled and false if not.
	// Also return error.
	scaleWithDesiredStrategy(int32, map[string]corev1.ResourceRequirements) (ifscale bool, err error)
}

type HorizontalScaleExecutor struct {
	ctx context.Context
	client.Client
	deployment                           *appsv1.Deployment
	currentReplicas                      int32
	scaleDownStabilizationWindowDuration *time.Duration
	lastScaleTime                        *metav1.Time
}

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

type VerticalScaleExecutor struct {
	ctx context.Context
	client.Client
	deployment                           *appsv1.Deployment
	currentReplicas                      int32
	scaleDownStabilizationWindowDuration *time.Duration
	lastScaleTime                        *metav1.Time
	verticalScalePolicy                  autoscalingv1.VerticalScalePolicy
}

func (v VerticalScaleExecutor) scaleWithDesiredStrategy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	switch v.verticalScalePolicy.Type {
	case autoscalingv1.VerticalScalePolicyTypeNormal:
		{
			return v.scaleWithNormalPolicy(desiredReplicas, desiredResourceRequirements)
		}
	case autoscalingv1.VerticalScalePolicyTypeAvailable:
		{
			return v.scaleWithAvailablePolicy(desiredReplicas, desiredResourceRequirements)
		}
	case autoscalingv1.VerticalScalePolicyTypeSafe:
		{
			return v.scaleWithSafePolicy(desiredReplicas, desiredResourceRequirements)
		}
	}
	return false, nil
}

func (v VerticalScaleExecutor) scaleWithNormalPolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	return false, nil
}

func (v VerticalScaleExecutor) scaleWithAvailablePolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	return false, nil
}

func (v VerticalScaleExecutor) scaleWithSafePolicy(desiredReplicas int32, desiredResourceRequirements map[string]corev1.ResourceRequirements) (ifscale bool, err error) {
	return false, nil
}
