package controllers

import (
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	autoscalingv1 "myw.domain/autoscaling/api/v1"
	metricsclient "myw.domain/autoscaling/metrics"
)

var (
	MinCPURequestQuantity    = resource.MustParse("25m")
	MinMemoryRequestQuantity = resource.MustParse("50Mi")
)

// ScaleDecisionMaker makes scale decision.
type ScaleDecisionMaker interface {
	// Computes desired replicas.
	computeDesiredReplicas() int32
	// Compute desired container resource requirements and pod request quantity.
	computeDesiredResourceRequirements() (map[string]corev1.ResourceRequirements, *resource.Quantity)
}

// HorizontalScaleDecisionMaker only makes horizontal scale decision.
type HorizontalScaleDecisionMaker struct {
	pods                          []corev1.Pod
	metrics                       metricsclient.PodMetricsInfo
	targetMetricSource            *autoscalingv1.MetricSource
	currentReplicas               int32
	minReplicas                   int32
	maxReplicas                   int32
	podRequestMilliValue          int64
	nextMetricStatus              *autoscalingv1.MetricStatus
	cpuInitializationPeriod       *time.Duration
	delayOfInitialReadinessStatus *time.Duration
	tolerance                     float32
}

// This computes desired replicas based on current status of pods.
func (h HorizontalScaleDecisionMaker) computeDesiredReplciasByCurrentStatus() (desiredReplicas int32, usage int64) {
	//Remove metrics from unready and ignored pods.
	targetResourceName := h.targetMetricSource.Name
	upperTargetUtilization := h.targetMetricSource.UpperTargetUtilization
	readyPodCount, unreadyPods, missingPods, ignoredPods := groupPods(h.pods, h.metrics, targetResourceName, *h.cpuInitializationPeriod, *h.delayOfInitialReadinessStatus)
	removeMetricsForPods(h.metrics, unreadyPods)
	removeMetricsForPods(h.metrics, ignoredPods)

	usageRatio, _, usage := calcUtilizationUsageRatio(h.metrics, h.podRequestMilliValue, upperTargetUtilization)
	scaledUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0

	// There's no unready pods or missing metrics.
	if !scaledUpWithUnready && len(missingPods) == 0 {
		// Difference is smaller than tolerance.
		// desiredReplicas = currentReplicas
		if math.Abs(float64(usageRatio)-1.0) <= float64(h.tolerance) {
			return h.currentReplicas, usage
		}
		// desiredReplicas = current usageRatio * readyPodCount
		return int32(math.Ceil(float64(usageRatio) * float64(readyPodCount))), usage
	}

	// Assume the metrics for missing and unready pods if any exists.
	// Missing metric was maximum in case of scaling down,
	// and zero in case of scaling up.
	// Metric from unready pod was zero.
	if len(missingPods) > 0 {
		if usageRatio < 1.0 {
			missingPodUtilization := int64(math.Max(100, float64(upperTargetUtilization)))
			for podName := range missingPods {
				h.metrics[podName] = metricsclient.PodMetric{Value: missingPodUtilization / 100 * h.podRequestMilliValue}
			}
		} else if usageRatio > 1.0 {
			for podName := range missingPods {
				h.metrics[podName] = metricsclient.PodMetric{Value: 0}
			}
		}
	}
	if scaledUpWithUnready {
		for podName := range unreadyPods {
			h.metrics[podName] = metricsclient.PodMetric{Value: 0}
		}
	}

	// There's unready pods or missing metrics.
	newUsageRatio, _, _ := calcUtilizationUsageRatio(h.metrics, h.podRequestMilliValue, upperTargetUtilization)
	// In several special cases,
	// desiredReplicas = currentReplicas
	if math.Abs(float64(newUsageRatio)-1.0) <= float64(h.tolerance) || (newUsageRatio > 1.0 && usageRatio < 1.0) || (newUsageRatio < 1.0 && usageRatio > 1.0) {
		return h.currentReplicas, usage
	}
	newReplicas := int32(math.Ceil(float64(newUsageRatio) * float64(len(h.metrics))))
	if newUsageRatio < 1.0 && newReplicas > h.currentReplicas || (newUsageRatio > 1.0 && newReplicas < h.currentReplicas) {
		return h.currentReplicas, usage
	}
	return newReplicas, usage
}

// This computes desired replicas based on predicted next status of pods.
func (h HorizontalScaleDecisionMaker) computeDesiredReplciasByPredictedStatus() (desiredReplicas int32) {
	upperTargetUtilization := h.targetMetricSource.UpperTargetUtilization
	nextUtilization := h.nextMetricStatus.CurrentUtilization
	metricLength := len(h.metrics)

	//predict the desired replicas for the upper target utilization
	nextUsageRatio := float64(nextUtilization) / float64(upperTargetUtilization)
	desiredReplicas = int32(math.Ceil(nextUsageRatio * float64(metricLength)))
	if math.Abs(nextUsageRatio-1.0) <= float64(h.tolerance) {
		return h.currentReplicas
	}
	return desiredReplicas
}

// This computes desired replicas based on both current and predicted status of pods.
func (h HorizontalScaleDecisionMaker) computeDesiredReplicas() int32 {
	currentDesiredReplicas, _ := h.computeDesiredReplciasByCurrentStatus()
	predictedDesiredReplicas := h.computeDesiredReplciasByPredictedStatus()
	if currentDesiredReplicas >= h.currentReplicas && predictedDesiredReplicas > currentDesiredReplicas {
		return h.resetDesiredReplicas(predictedDesiredReplicas)
	}
	return h.resetDesiredReplicas(currentDesiredReplicas)
}

func (h HorizontalScaleDecisionMaker) resetDesiredReplicas(desiredReplicas int32) int32 {
	if desiredReplicas >= h.minReplicas && desiredReplicas <= h.maxReplicas {
		return desiredReplicas
	} else if desiredReplicas < h.minReplicas {
		return h.minReplicas
	} else {
		return h.maxReplicas
	}
}

// This computes the desired resource requirements of containers and pod request quantity.
// For horizontal scaling, desired resource requirements are the same with the current ones.
func (h HorizontalScaleDecisionMaker) computeDesiredResourceRequirements() (map[string]corev1.ResourceRequirements, *resource.Quantity) {
	podSample := h.pods[0]
	targetResourceName := h.targetMetricSource.Name
	const multiplier = float64(1.0)
	return calcMultipliedResourceRequirementsMap(podSample, targetResourceName, multiplier),
		calcMultipliedRequestQuantity(h.podRequestMilliValue, targetResourceName, multiplier)
}

// VerticalScaleDecisionMaker only makes vertical scale decision.
type VerticalScaleDecisionMaker struct {
	pods                          []corev1.Pod
	metrics                       metricsclient.PodMetricsInfo
	currentReplicas               int32
	podRequestMilliValue          int64
	nextMetricStatus              *autoscalingv1.MetricStatus
	targetMetricSource            *autoscalingv1.MetricSource
	cpuInitializationPeriod       *time.Duration
	delayOfInitialReadinessStatus *time.Duration
	tolerance                     float32
}

// This computes desired replicas.
// For vertical scaling, the desired replicas count is the same with the current one.
func (v VerticalScaleDecisionMaker) computeDesiredReplicas() int32 {
	return v.currentReplicas
}

// This computes the desired resource requirements of containers and pod request quantity.
func (v VerticalScaleDecisionMaker) computeDesiredResourceRequirements() (map[string]corev1.ResourceRequirements, *resource.Quantity) {
	currentDesiredResourceRequirements, currentDesiredRequestQuantity := v.computeDesiredResourceRequirementsByCurrentStatus()
	predictedDesiredResourceRequirements, predictedDesiredRequestQuantity := v.computeDesiredResourceRequirementsByPredictedStatus()
	if currentDesiredRequestQuantity.MilliValue() >= v.podRequestMilliValue && predictedDesiredRequestQuantity.MilliValue() > currentDesiredRequestQuantity.MilliValue() {
		return predictedDesiredResourceRequirements, &predictedDesiredRequestQuantity
	}
	return currentDesiredResourceRequirements, currentDesiredRequestQuantity
}

// This computes the desired resource requirements of containers and pod request quantity based on current status of pods.
func (v VerticalScaleDecisionMaker) computeDesiredResourceRequirementsByCurrentStatus() (map[string]corev1.ResourceRequirements, *resource.Quantity) {
	podSample := *v.pods[0].DeepCopy()
	targetResourceName := v.targetMetricSource.Name
	lowerTargetUtilization := v.targetMetricSource.LowerTargetUtilization
	resourceRequirementsMap := calcMultipliedResourceRequirementsMap(podSample, targetResourceName, 1.0)
	requestQuantity := calcMultipliedRequestQuantity(v.podRequestMilliValue, targetResourceName, 1.0)

	//Remove metrics from unready and ignored pods.
	_, unreadyPods, missingPods, ignoredPods := groupPods(v.pods, v.metrics, targetResourceName, *v.cpuInitializationPeriod, *v.delayOfInitialReadinessStatus)
	removeMetricsForPods(v.metrics, unreadyPods)
	removeMetricsForPods(v.metrics, ignoredPods)

	usageRatio, _, _ := calcUtilizationUsageRatio(v.metrics, v.podRequestMilliValue, lowerTargetUtilization)
	scaledUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0

	// There's no unready pods or missing metrics.
	if !scaledUpWithUnready && len(missingPods) == 0 {
		// Difference is smaller than tolerance.
		// desired requests/limits = current requests/limits
		if math.Abs(float64(usageRatio)-1.0) <= float64(v.tolerance) {
			return resourceRequirementsMap, requestQuantity
		}
		// desired requests/limits = current requests/limits * usageRatio
		desiredResourceRequirementsMap := calcMultipliedResourceRequirementsMap(podSample, targetResourceName, float64(usageRatio))
		desiredRequestQuantity := calcMultipliedRequestQuantity(v.podRequestMilliValue, targetResourceName, float64(usageRatio))

		return desiredResourceRequirementsMap, desiredRequestQuantity
	}

	// Assume the metrics for missing and unready pods if any exists.
	// Missing metric was maximum in case of scaling down,
	// and zero in case of scaling up.
	// Metric from unready pod was zero.
	if len(missingPods) > 0 {
		if usageRatio < 1.0 {
			missingPodUtilization := int64(math.Max(100, float64(lowerTargetUtilization)))
			for podName := range missingPods {
				v.metrics[podName] = metricsclient.PodMetric{Value: missingPodUtilization / 100 * v.podRequestMilliValue}
			}
		} else if usageRatio > 1.0 {
			for podName := range missingPods {
				v.metrics[podName] = metricsclient.PodMetric{Value: 0}
			}
		}
	}
	if scaledUpWithUnready {
		for podName := range unreadyPods {
			v.metrics[podName] = metricsclient.PodMetric{Value: 0}
		}
	}

	// There's unready pods or missing metrics.
	newUsageRatio, _, _ := calcUtilizationUsageRatio(v.metrics, v.podRequestMilliValue, lowerTargetUtilization)
	// In several special cases,
	// desired requests/limits = current requests/limits
	if math.Abs(float64(newUsageRatio)-1.0) <= float64(v.tolerance) || (newUsageRatio > 1.0 && usageRatio < 1.0) || (newUsageRatio < 1.0 && usageRatio > 1.0) {
		return resourceRequirementsMap, requestQuantity
	}
	// desired requests/limits = current requests/limits * newUsageRatio
	desiredResourceRequirementsMap := calcMultipliedResourceRequirementsMap(podSample, targetResourceName, float64(newUsageRatio))
	desiredRequestQuantity := calcMultipliedRequestQuantity(v.podRequestMilliValue, targetResourceName, float64(newUsageRatio))
	return desiredResourceRequirementsMap, desiredRequestQuantity
}

// This computes the desired resource requirements of containers and pod request quantity based on predicted status of pods.
func (v VerticalScaleDecisionMaker) computeDesiredResourceRequirementsByPredictedStatus() (map[string]corev1.ResourceRequirements, resource.Quantity) {
	podSample := *v.pods[0].DeepCopy()
	lowerTargetUtilization := v.targetMetricSource.LowerTargetUtilization
	targetResourceName := v.targetMetricSource.Name
	nextUtilization := v.nextMetricStatus.CurrentUtilization

	//calculate
	resourceRequirementsMap := calcMultipliedResourceRequirementsMap(podSample, targetResourceName, 1.0)
	requestQuantity := calcMultipliedRequestQuantity(v.podRequestMilliValue, targetResourceName, 1.0)

	// Predict the desired requests/limits for the lower target utilization.
	nextUsageRatio := float64(nextUtilization) / float64(lowerTargetUtilization)
	// When difference is within tolerance,
	// desired requests/limits = current requests/limits
	if math.Abs(nextUsageRatio-1.0) <= float64(v.tolerance) {
		return resourceRequirementsMap, *requestQuantity
	}
	//desired requests/limits = current requests/limits * usageRatio
	desiredResourceRequirementsMap := calcMultipliedResourceRequirementsMap(podSample, targetResourceName, nextUsageRatio)
	desiredRequestQuantity := calcMultipliedRequestQuantity(v.podRequestMilliValue, targetResourceName, nextUsageRatio)
	return desiredResourceRequirementsMap, *desiredRequestQuantity
}

// Calculate resource requirements of containers after multiplying the target resource requirements.
func calcMultipliedResourceRequirementsMap(podSample corev1.Pod, targetResourceName corev1.ResourceName, multiplier float64) map[string]corev1.ResourceRequirements {
	// Initialize the resourceRequirementsMap from container name to resourece requirements
	resourceRequirementsMap := make(map[string]corev1.ResourceRequirements)
	for _, c := range podSample.Spec.Containers {
		resourceRequirementsMap[c.Name] = *c.Resources.DeepCopy()
	}

	// With multiplier 1.0, desired requests/limits = current requests/limits.
	if multiplier == 1.0 {
		return resourceRequirementsMap
	}

	// With multiplier other value, modify requests/limits.
	// desired requests/limits = usageRatio * current requests/limits
	if targetResourceName == corev1.ResourceCPU {
		for n, r := range resourceRequirementsMap {
			cpuRequestQuantityMilliValue := int64(float32(r.Requests.Cpu().MilliValue()) * float32(multiplier))
			cpuLimitQuantityMilliValue := int64(float32(r.Limits.Cpu().MilliValue()) * float32(multiplier))
			r.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(cpuRequestQuantityMilliValue, resource.DecimalSI)
			r.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(cpuLimitQuantityMilliValue, resource.DecimalSI)
			resourceRequirementsMap[n] = r
		}
	} else if targetResourceName == corev1.ResourceMemory {
		for n, r := range resourceRequirementsMap {
			memoryRequestQuantityMilliValue := int64(float32(r.Requests.Cpu().MilliValue()) * float32(multiplier))
			memoryLimitQuantityMilliValue := int64(float32(r.Limits.Cpu().MilliValue()) * float32(multiplier))
			r.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(memoryRequestQuantityMilliValue, resource.BinarySI)
			r.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(memoryLimitQuantityMilliValue, resource.BinarySI)
			resourceRequirementsMap[n] = r
		}
	}
	return resourceRequirementsMap
}

// Calculate request quantity after multiplying it.
func calcMultipliedRequestQuantity(requestQuantityMilliValue int64, targetResourceName corev1.ResourceName, multiplier float64) *resource.Quantity {
	multipliedRequestQuantityMilliValue := int64(float64(requestQuantityMilliValue) * multiplier)
	if targetResourceName == corev1.ResourceCPU {
		return resource.NewMilliQuantity(multipliedRequestQuantityMilliValue, resource.DecimalSI)
	} else { //targetResourceName == corev1.ResourceMemory
		return resource.NewMilliQuantity(multipliedRequestQuantityMilliValue, resource.BinarySI)
	}
}
