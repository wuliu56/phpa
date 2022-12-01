/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CrossVersionObjectReference contains the information to get the referred resource
type CrossVersionObjectReference struct {
	// Kind of the referent
	Kind string `json:"kind"`

	// Name of the referent
	Name string `json:"name"`

	// API version of the referent
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
}

type MetricSource struct {
	// The name of the resource in question.
	Name v1.ResourceName `json:"name"`

	// The lower target utilization of the average of the resource metric across all relevant
	// pods, represented as a percentage of the requested value of the resource for the pods.
	// Defaults to 0 and minimum is 0.
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	LowerTargetUtilization int32 `json:"lowerTargetUtilization"`

	// The upper target utilization of the average of the resource metric across all relevant
	// pods, represented as a percentage of the requested value of the resource for the pods.
	// Defaults to 80 and minimum is 0.
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=0
	// +optional
	UpperTargetUtilization int32 `json:"upperTargetUtilization"`
}

type MetricStatus struct {
	// The name of the resource in question.
	Name v1.ResourceName `json:"name"`

	// The current value of the resource metric across all relevant pods,
	// represented as a percentage of the requested value of the resource for the pods.
	CurrentUtilization int32 `json:"currentUtilization"`

	// The current value of the resource metric across all relevant pods,
	// represented as the raw value sum of the requested value of the resource for the pods.
	CurrentValue *resource.Quantity `json:"currentValue,omitempty"`
}

// ScaleEvent records the information of a scaling event.
type ScaleEvent struct {
	//The time when a scaling happens.
	Time *metav1.Time `json:"time"`

	//Type of scaling.
	//Possible values are:
	//- "Horizontal"
	//- "Vertical"
	Type string `json:"type"`

	//Count of desired replicas.
	Replicas int32 `json:"replicas"`

	//The desired pod request.
	Request *resource.Quantity `json:"request"`
}

// Specifies the mode of scaling.
// Can only be one of the following modes.
// +kubebuilder:validation:Enum=Horizontal;Vertical;Hybrid
// +kubebuilder:default=Hybrid
type ScaleMode string

const (
	// Only horizontally scale in this mode.
	ScaleModeHorizontal ScaleMode = "Horizontal"

	// Only vertically scale in this mode.
	ScaleModeVertical ScaleMode = "Vertical"

	// Both horizontally and vertically scale in this mode.
	ScaleModeHybrid ScaleMode = "Hybrid"
)

// Specifies the policy of horizontal scaling.
type HorizontalScalePolicy struct {
	// Alpha for the DES algorithm. If not explicitly specified,
	// the auto adjustment will be applied to alpha according to collected metrics.
	// +optional
	Alpha string `json:"alpha,omitempty"`
}

// The policy type of vertical scaling.
// Can only be one of the following policies.
// Default is Normal.
// +kubebuilder:validation:Enum=Normal;Available;Safe
// +kubebuilder:default=Normal
type VerticalScalePolicyType string

const (
	// Replace the old pods by new ones using rolling update.
	VerticalScalePolicyTypeNormal VerticalScalePolicyType = "Normal"

	// Create more replicas before scaling in order to ensure availability of the target workload.
	VerticalScalePolicyTypeAvailable VerticalScalePolicyType = "Available"

	// Replace the old workload by new one using rolling update.
	// A service pointing to the target workload is necessary to use this type.
	VerticalScalePolicyTypeSafe VerticalScalePolicyType = "Safe"
)

// Specifies container resource policy.
type ContainerResourcePolicy struct {
	// Name of the container or DefaultContainerResourcePolicy, in which
	// case the policy is used by the containers that don't have their own
	// policy specified.
	ContainerName string `json:"containerName,omitempty"`

	// Specifies the minimal amount of resources that the container can be
	// scaled down to. The default is no minimum.
	// +optional
	MinAllowed v1.ResourceList `json:"minAllowed,omitempty"`

	// Specifies the maximal amount of resources that the container can be
	// scaled up to. The default is no maximum.
	// +optional
	MaxAllowed v1.ResourceList `json:"maxAllowed,omitempty"`
}

// Specifies the policy of vertical scaling.
type VerticalScalePolicy struct {
	// The policy type of vertical scaling.
	// Possible values are:
	// - "Normal"
	// - "Available"
	// - "Safe"
	// Defaults to 'Normal'
	Type VerticalScalePolicyType `json:"type"`

	// Fraction of replica count that can be evicted for update, if more than one pod can be evicted.
	// Represented as a percentage.
	// Defaults to 50 and minimum is 1.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=1
	// +optional
	EvictionTolerance int32 `json:"evictionTolerance"`

	// Per-container resource policies.
	// +optional
	ContainerResourcePolicies []ContainerResourcePolicy `json:"containerResourcePolicies,omitempty"`

	// Label selector of the service that points to the target pods.
	// +optional
	ServiceLabelSelector map[string]string `json:"serviceLabelSelector,omitempty"`
}

// PredictiveHorizontalPodAutoscalerSpec defines the desired state of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Refers to the workload resource to be scaled.
	ScaleTargetRef *CrossVersionObjectReference `json:"scaleTargetRef"`

	// The target metric used to calculate the desired replica count.
	// Here multi-metrics haven't been applied, and only resource metrics are supported.
	Metrics *MetricSource `json:"metrics"`

	// The lower limit for the number of replicas to which the autoscaler can scale down.
	// Defaults to 1 and minimum is 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	MinReplicas int32 `json:"minReplicas"`

	// The upper limit for the number of replicas to which the autoscaler can scale up.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// Specifies the mode of scaling.
	// Possible values are:
	// - "Horizontal"
	// - "Vertical"
	// - "Hybrid"
	// Defaults to 'Hybrid'
	Mode ScaleMode `json:"mode"`

	// The policy of vertical scaling.
	// +optional
	VerticalScalePolicy *VerticalScalePolicy `json:"verticalScalePolicy,omitempty"`

	// The policy of horizontal scaling.
	// +optional
	HorizontalScalePolicy *HorizontalScalePolicy `json:"horizontalScalePolicy,omitempty"`

	// The interval number of the monitor window used for resource prediction.
	// Defaults to 20 and minimum is 2.
	// +kubebuilder:default=20
	// +kubebuilder:validation:Minimum=2
	// +optional
	MonitorWindowIntervalNum int32 `json:"monitorWindowIntervalNum"`

	// The minimal interval(second) between scaling down.
	// Defaults to 120 and minimum is 0.
	// +kubebuilder:default=120
	// +kubebuilder:validation:Minimum=0
	// +optional
	ScaleDownStabilizationWindowSeconds int32 `json:"scaleDownStabilizationWindowSeconds"`

	// Only to scale when tolerance is smaller than the difference between current utilization and target range.
	// Represented as a percentage.
	// Defaults to 10 and minimum is 0.
	// This is a pointer to distinguish between explicit zero and not specified.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Tolerance *int32 `json:"tolerance,omitempty"`

	// CpuInitializationPeriod defines the period seconds after the pod starting when the pod is assumed being initialized,
	// and thus any transition into readiness is the first one.
	// Default to 5 minutes.
	// +optional
	CpuInitializationPeriod string `json:"cpuInitializationPeriod,omitempty"`

	// DelayOfInitialReadinessStatus defines the period seconds after the pod starting when the pod is assumed to be
	// still unready after last transition into unreadiness.
	// Defaults to 30 seconds.
	// +optional
	DelayOfInitialReadinessStatus string `json:"delayOfInitialReadinessStatus,omitempty"`

	// The limit for the number of scaling history records.
	// Defaults to 20 and minimum is 0.
	// This is a pointer to distinguish between explicit zero and not specified.
	// +kubebuilder:validation:Minimum=0
	// +optional
	ScaleHistoryLimit *int32 `json:"scaleHistoryLimit,omitempty"`
}

// PredictiveHorizontalPodAutoscalerStatus defines the observed state of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// The current number of replicas of the target workload.
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`

	// The desired number of replicas of the target workload.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`

	// Contains the collected metrics within the monitor window.
	// +optional
	MetricsList []MetricStatus `json:"metricsList"`

	// Contains the recent scaling events.
	// +optional
	ScaleEventsList []ScaleEvent `json:"scaleEventsList"`

	// The last time the PredictiveHorizontalPodAutoscaler scales, used by the
	// autoscaler to control the scaling frequency.
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// Refers to the time when the last metrics are fetched and used to update PredictiveHorizontalPodAutoscalerStatus.
	// +optional
	LastMonitorTime *metav1.Time `json:"lastMonitorTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=phpa
// PredictiveHorizontalPodAutoscaler is the Schema for the predictivehorizontalpodautoscalers API
type PredictiveHorizontalPodAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PredictiveHorizontalPodAutoscalerSpec   `json:"spec,omitempty"`
	Status PredictiveHorizontalPodAutoscalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PredictiveHorizontalPodAutoscalerList contains a list of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PredictiveHorizontalPodAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PredictiveHorizontalPodAutoscaler{}, &PredictiveHorizontalPodAutoscalerList{})
}
