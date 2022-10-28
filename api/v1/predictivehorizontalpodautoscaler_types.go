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
	//Kind of the referent
	Kind string `json:"kind"`

	//Name of the referent
	Name string `json:"name"`

	//API version of the referent
	//+optional
	APIVersion string `json:"apiVersion,omitempty"`
}

type MetricSource struct {
	//Name is the name of the resource in question.
	Name v1.ResourceName `json:"name"`

	//TargetAverageUtilization is the target value of the average of the resource metric across all relevant
	//pods, represented as a percentage of the requested value of the resource for the pods.
	//+optional
	TargetAverageUtilization *int32 `json:"targetAverageUtilization,omitempty"`

	//TargetAverageValue is the target value of the resource metric across all relevant pods, as a raw value.
	//+optional
	TargetAverageValue *resource.Quantity `json:"targetAverageValue,omitempty"`
}

type MetricStatus struct {
	//Name is the name of the resource in question.
	Name v1.ResourceName `json:"name"`

	//CurrentUtilization is the current value of the resource metric across all relevant
	//pods, represented as a percentage of the requested value of the resource for the pods.
	//It will only be present if `targetAverageUtilization` was set in the corresponding metric specification.
	//+optional
	CurrentUtilization *int32 `json:"currentUtilization,omitempty"`

	//CurrentAverageUtilization is the current value of the resource metric across all relevant
	//pods, represented as the raw value of the requested value of the resource for the pods.
	//It will always be present.
	CurrentValue *resource.Quantity `json:"currentValue,omitempty"`
}

// ScaleEvent records the information of a scaling event, including its happening time and desired replicas.
type ScaleEvent struct {
	//HappeningTime is the time when a scaling happens.
	Time *metav1.Time `json:"time"`

	//Replicas is the replica count of a scaling.
	Replicas *int32 `json:"replicas"`
}

// PredictiveHorizontalPodAutoscalerSpec defines the desired state of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//ScaleTargetRef refers to the workload resource to be scaled.
	ScaleTargetRef CrossVersionObjectReference `json:"scaleTargetRef"`

	//MinReplicas is the lower limit for the number of replicas to which the autoscaler can scale down.
	//It defaults to 1 pod.
	//+kubebuilder:default=1
	//+optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	//MaxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	//+kubebuilder:validation:Minimum=1
	MaxReplicas *int32 `json:"maxReplicas"`

	//Metrics contains the specifications for which to use to to calculate the desired replica count.
	//Here multi-metrics haven't been applied, and only resource metrics are supported.
	//+optional
	Metrics *MetricSource `json:"metrics,omitempty"`

	//ScaleDownStabilizationWindowSeconds refers to the minimum interval(second) between pod scaling down.
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:default=120
	//+optional
	ScaleDownStabilizationWindowSeconds *int32 `json:"scaleDownStabilizationWindowSeconds,omitempty"`

	//MetricsCollectionInterval refers to the interval(second) at which the specified pod resource metrics are collected
	//for predictive scaling. It defaults to 10 seconds.
	//+kubebuilder:validation:Minimum=10
	//+kubebuilder:default=10
	//+optional
	//MetricsCollectionInterval *int32 `json:"metricsCollectionInterval,omitempty"`

	//MonitorWindowIntervalNum refers to the interval number of the monitor window used for pod resource prediction.
	//+kubebuilder:validation:Minimum=2
	//+kubebuilder:default=20
	//+optional
	MonitorWindowIntervalNum *int32 `json:"monitorWindowIntervalNum,omitempty"`

	//Alpha defines a fixed alpha parameter for the DES algorithm. If not explicitly specified,
	//an auto adjustment will be applied to alpha according to collected metrics.
	//+optional
	Alpha *string `json:"alpha"`

	//Tolerance decides whether to scale pods if resource usage isn't that larger than the threshold.
	//+optional
	Tolerance *string `json:"tolerance"`

	//CpuInitializationPeriod defines the period seconds after the pod starting when the pod is assumed being initialized,
	//and thus any transition into readiness is the first one.
	//+optional
	CpuInitializationPeriod *string `json:"cpuInitializationPeriod,omitempty"`

	//DelayOfInitialReadinessStatus defines the period seconds after the pod starting when the pod is assumed to be
	//still unready after last transition into unreadiness.
	//+optional
	DelayOfInitialReadinessStatus *string `json:"delayOfInitialReadinessStatus,omitempty"`

	//ScaleHistoryLimit refers to the limit for the number of scaling history records. It defaults to 50 pieces.
	//+kubebuilder:validation:Minimum=0
	//+kubebuilder:default=20
	//+optional
	ScaleHistoryLimit *int32 `json:"scaleHistoryLimit"`
}

// PredictiveHorizontalPodAutoscalerStatus defines the observed state of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//lastScaleTime is the last time the HorizontalPodAutoscaler scaled the number of pods, used by the
	//autoscaler to control the scaling frequency.
	//+optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	//CurrentReplicas is current number of replicas of pods manager by this autoscaler, as last seen by the autoscaler.
	CurrentReplicas *int32 `json:"currentReplicas"`

	//desiredReplicas is the desired number of replicas of pods managered by this autoscaler.
	DesiredReplicas *int32 `json:"desiredReplicas"`

	//MetricsList contains the collected metrics within the monitor window.
	//+optional
	MetricsList []MetricStatus `json:"metricsList"`

	//ScaleEventsList contains the recent scaling events.
	//+optional
	ScaleEventsList []ScaleEvent `json:"scaleEventsList"`

	//LastMonitorTime refers to the time when the last metrics are fetched and used to update PredictiveHorizontalPodAutoscalerStatus
	//+optional
	LastMonitorTime *metav1.Time `json:"lastMonitorTime"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PredictiveHorizontalPodAutoscaler is the Schema for the predictivehorizontalpodautoscalers API
type PredictiveHorizontalPodAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PredictiveHorizontalPodAutoscalerSpec   `json:"spec,omitempty"`
	Status PredictiveHorizontalPodAutoscalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PredictiveHorizontalPodAutoscalerList contains a list of PredictiveHorizontalPodAutoscaler
type PredictiveHorizontalPodAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PredictiveHorizontalPodAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PredictiveHorizontalPodAutoscaler{}, &PredictiveHorizontalPodAutoscalerList{})
}
