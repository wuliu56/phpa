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

package controllers

import (
	"context"
	"fmt"
	"math"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	autoscalingv1 "myw.domain/autoscaling/api/v1"
	metricsclient "myw.domain/autoscaling/metrics"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PredictiveHorizontalPodAutoscalerReconciler reconciles a PredictiveHorizontalPodAutoscaler object
type PredictiveHorizontalPodAutoscalerReconciler struct {
	Config *rest.Config
	client.Client
	Scheme *runtime.Scheme
	// MonitorInterval is used to control the rate that the phpa status is updated
	// and the replica count of workload spec is changed.
	MonitorInterval               time.Duration
	CpuInitializationPeriod       time.Duration
	DelayOfInitialReadinessStatus time.Duration
	Tolerance                     float32
	ScaleHistoryLimit             int32
}

//+kubebuilder:rbac:groups=autoscaling.myw.domain,resources=predictivehorizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.myw.domain,resources=predictivehorizontalpodautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.myw.domain,resources=predictivehorizontalpodautoscalers/finalizers,verbs=update
//+kubebuilder:rabc:groups=apps,resources=deployments,verbs=get;list;update
//+kubebuilder:rabc:groups=apps,resources=deployments/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PredictiveHorizontalPodAutoscaler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *PredictiveHorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	// TODO(user): your logic here

	// Get the PredictiveHorizontalPodAutoscaler.
	log.V(1).Info("fetching PredictiveHorizontalPodAutoscaler")
	var phpa autoscalingv1.PredictiveHorizontalPodAutoscaler
	if err := r.Get(ctx, req.NamespacedName, &phpa); err != nil {
		log.Error(err, "unable to fetch PredictiveHorizontalPodAutoscaler")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.V(1).Info("successfully fetched PredictiveHorizontalPodAutoscaler", "PredictiveHorizontalPodAutoscaler", phpa.Namespace+"/"+phpa.Name)

	// Fetch fields of PredictiveHorizontalPodAutoscaler.
	spec := phpa.Spec.DeepCopy()
	status := phpa.Status.DeepCopy()
	maxReplicas := spec.MaxReplicas
	minReplicas := spec.MinReplicas
	// scaleDownStabilizationWindowSeconds := spec.ScaleDownStabilizationWindowSeconds
	scaleTargetRef := spec.ScaleTargetRef
	targetMetricSource := spec.Metrics
	mode := spec.Mode

	// Judge if a new round of monitoring is necessary.
	// If not, end the reconcile.
	lastMonitorTime := status.LastMonitorTime
	if lastMonitorTime != nil {
		if lastMonitorTime.Time.Add(r.MonitorInterval).After(time.Now()) {
			log.V(0).Info("too short interval to reconcile")
			return ctrl.Result{}, nil
		}
	}

	// Initialize cpuInitializationPeriod, delayOfInitialReadinessStatus, tolerance and scaleHistoryLimit if specified in yaml.
	if spec.CpuInitializationPeriod != "" {
		var err error
		r.CpuInitializationPeriod, err = time.ParseDuration(spec.CpuInitializationPeriod)
		if err != nil {
			log.Error(err, "unable to parse CpuInitializationPeriod in the format of time.Duration")
			return ctrl.Result{}, err
		}
	}
	if spec.DelayOfInitialReadinessStatus != "" {
		var err error
		r.DelayOfInitialReadinessStatus, err = time.ParseDuration(spec.DelayOfInitialReadinessStatus)
		if err != nil {
			log.Error(err, "unable to parse DelayOfInitialReadinessStatus in the format of time.Duration")
			return ctrl.Result{}, err
		}
	}
	if spec.Tolerance != nil {
		r.Tolerance = float32(*spec.Tolerance) / 100.0
	}
	if spec.ScaleHistoryLimit != nil {
		r.ScaleHistoryLimit = *spec.ScaleHistoryLimit
	}

	// Get the resource for the target reference.
	// In test version, only Deployment is used as target workload.
	var deployment appsv1.Deployment
	namespace := req.Namespace
	deploymentName := scaleTargetRef.Name
	targetGVR := "apps/v1/deployment"
	log.V(1).Info("fetching the target workload", "target GVR", targetGVR, "namespace", namespace, "deployment name", deploymentName)
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      deploymentName,
	}, &deployment); err != nil {
		log.Error(err, "unable to fetch the target workload", "target GVR", targetGVR, "namespace", namespace, "deployment name", deploymentName)
		return ctrl.Result{}, err
	}
	log.V(1).Info("successfully fetched the target workload", "target GVR", targetGVR, "namespace", namespace, "deployment name", deploymentName)

	// List the pods for the target workload.
	var podList corev1.PodList
	selector := getDeploymentSelector(&deployment)
	log.V(1).Info("listing pods for target workload", "selector", selector)
	if err := r.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: *selector}); err != nil {
		log.Error(err, "unable to list pods for target workload", "selector", selector)
		return ctrl.Result{}, err
	}
	log.V(1).Info("successfully listed pods for target workload", "selector", selector)

	// Construct metricsClient and fetch metrics.
	// In test version, only raw pod metrics are fetched.
	resourceClient := v1beta1.NewForConfigOrDie(r.Config)
	metricsClient := metricsclient.NewRESTMetricsClient(
		resourceClient,
		nil,
		nil,
	)
	resourceName := spec.Metrics.Name
	log.V(1).Info("fetching the current target metrics from metricsClient", "resource name", resourceName)
	metrics, _, err := metricsClient.GetResourceMetric(ctx, resourceName, namespace, *selector, "")
	if err != nil {
		log.Error(err, "unable to fetch the current target metrics from metricsClient", "resource name", resourceName)
	}
	log.V(1).Info("successfully fetched the current target metrics from metricsClient", "resource name", resourceName)

	// Compute pod resource request.
	currentReplicas := deployment.Status.Replicas
	podRequest, err := calcPodResourceRequest(&podList.Items[0], resourceName)
	if err != nil {
		log.Error(err, "unable to compute pod resource request")
		return ctrl.Result{}, err
	}

	// Construct the new metric status list and predict the next metric.
	currentMetricStatus, metricsLength := r.getCurrentMetricStatus(podList.Items, metrics, podRequest, resourceName, mode, targetMetricSource)
	totalRequest := podRequest * int64(metricsLength)
	constructPHPAMetricsList(&phpa, currentMetricStatus)
	log.V(1).Info("predict the metric at the next interval")
	nextMetricStatus, err := predictNextMetricStatusByADES(&phpa, totalRequest)
	if err != nil {
		log.Error(err, "failed to predict the metric at the next interval")
		return ctrl.Result{}, err
	}

	// Create the scale decision maker and scale executor according to the mode.
	var scaleDecisionMaker ScaleDecisionMaker
	// var scaleExecutor ScaleExecutor
	switch spec.Mode {
	case autoscalingv1.ScaleModeHorizontal:
		{
			scaleDecisionMaker = HorizontalScaleDecisionMaker{
				pods:                          podList.Items,
				metrics:                       metrics,
				currentReplicas:               currentReplicas,
				minReplicas:                   minReplicas,
				maxReplicas:                   maxReplicas,
				podRequestMilliValue:          podRequest,
				nextMetricStatus:              nextMetricStatus,
				targetMetricSource:            targetMetricSource,
				cpuInitializationPeriod:       &r.CpuInitializationPeriod,
				delayOfInitialReadinessStatus: &r.DelayOfInitialReadinessStatus,
				tolerance:                     r.Tolerance,
			}
			// scaleDownStabilizationWindowDuration := time.Duration(scaleDownStabilizationWindowSeconds) * time.Second
			// scaleExecutor = HorizontalScaleExecutor{
			// 	ctx:                                  ctx,
			// 	Client:                               r.Client,
			// 	deployment:                           &deployment,
			// 	currentReplicas:                      currentReplicas,
			// 	scaleDownStabilizationWindowDuration: &scaleDownStabilizationWindowDuration,
			// 	lastScaleTime:                        phpa.Status.LastScaleTime,
			// }
		}
	case autoscalingv1.ScaleModeVertical:
		{
			scaleDecisionMaker = VerticalScaleDecisionMaker{
				pods:                          podList.Items,
				metrics:                       metrics,
				currentReplicas:               currentReplicas,
				podRequestMilliValue:          podRequest,
				nextMetricStatus:              nextMetricStatus,
				targetMetricSource:            targetMetricSource,
				cpuInitializationPeriod:       &r.CpuInitializationPeriod,
				delayOfInitialReadinessStatus: &r.DelayOfInitialReadinessStatus,
				tolerance:                     r.Tolerance,
				verticalScalePolicy:           *phpa.Spec.VerticalScalePolicy,
			}
		}
	}

	// Decide the final desired replicas based on both the current and predicted metrics.
	desiredReplicas := scaleDecisionMaker.computeDesiredReplicas()
	desiredResourceRequirements, desiredPodRequestQuantity := scaleDecisionMaker.computeDesiredResourceRequirements()
	fmt.Printf("current metric status: %v\n", currentMetricStatus)
	fmt.Printf("predicted next metric status: %v\n", nextMetricStatus)
	fmt.Printf("current resources requirements: \n")
	for i, c := range podList.Items[0].Spec.Containers {
		fmt.Printf("resources of %vth container: %v\n", i, c.Resources)
	}
	fmt.Printf("desired resources requirements: %v\n", desiredResourceRequirements)
	fmt.Printf("current pod request quantity: %v\n", podRequest)
	fmt.Printf("desired pod request quantity: %v\n", desiredPodRequestQuantity)

	phpa.Status.CurrentReplicas = currentReplicas
	phpa.Status.DesiredReplicas = desiredReplicas
	phpa.Status.DesiredResourceRequirements = desiredResourceRequirements

	// Do the scaling and record the scale event.
	// ifScaled, err := scaleExecutor.scaleWithDesiredStrategy(desiredReplicas, desiredResourceRequirements)
	// if err != nil {
	// 	log.Error(err, "failed to scale")
	// 	return ctrl.Result{}, err
	// }
	// if ifScaled {
	// 	nowTime := metav1.Time{Time: time.Now()}
	// 	phpa.Status.LastScaleTime = &nowTime
	// 	newScaleEvent := autoscalingv1.ScaleEvent{
	// 		Time:     &nowTime,
	// 		Type:     "Horizontal",
	// 		Replicas: desiredReplicas,
	// 		Request:  desiredPodRequestQuantity,
	// 	}
	// 	r.recordScaleEvent(&phpa, newScaleEvent)
	// 	log.V(0).Info("successfully scaled", "scale event", newScaleEvent)
	// } else {
	// 	log.V(0).Info("not to scale because of the same desired replicas with current replicas or too often scaling down")
	// }

	// Add a label to phpa as deployment reference
	if phpa.Labels == nil {
		phpa.Labels = make(map[string]string)
	}
	if _, exists := phpa.Labels["target-reference"]; !exists {
		phpa.Labels["target-reference"] = namespace + "-" + deploymentName
		if err := r.Update(ctx, &phpa); err != nil {
			log.Error(err, "unable to add the label of target reference")
			return ctrl.Result{}, err
		}
	}

	// Update the phpa status.
	if lastMonitorTime != nil {
		log.V(0).Info("updating the phpa status", "time now", time.Now(), "time interval from last monitor", time.Since(lastMonitorTime.Time))
	}
	phpa.Status.LastMonitorTime = &metav1.Time{Time: time.Now()}
	if err := r.Status().Update(ctx, &phpa); err != nil {
		log.Error(err, "unable to update the phpa status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// recordScaleEvent records a scale event.
func (r *PredictiveHorizontalPodAutoscalerReconciler) recordScaleEvent(phpa *autoscalingv1.PredictiveHorizontalPodAutoscaler, scaleEvent autoscalingv1.ScaleEvent) {
	scaleEventsList := phpa.Status.ScaleEventsList
	if scaleEventsList == nil {
		phpa.Status.ScaleEventsList = make([]autoscalingv1.ScaleEvent, 0)
	}
	scaleEventNum := int32(len(scaleEventsList))
	if scaleEventNum < r.ScaleHistoryLimit {
		phpa.Status.ScaleEventsList = append(scaleEventsList, scaleEvent)
	} else {
		scaleEventsList = scaleEventsList[1:r.ScaleHistoryLimit:r.ScaleHistoryLimit]
		phpa.Status.ScaleEventsList = append(scaleEventsList, scaleEvent)
	}
}

// Predict the metrics at the next interval using adaptive DES algorithm.
// Return MetricStatus and possible error.
func predictNextMetricStatusByADES(phpa *autoscalingv1.PredictiveHorizontalPodAutoscaler, totalRequest int64) (*autoscalingv1.MetricStatus, error) {
	metricList := phpa.Status.MetricsList
	if len(metricList) == 0 {
		return nil, fmt.Errorf("there's no metric provided for prediction")
	}
	metricNum := len(phpa.Status.MetricsList)
	metricName := metricList[0].Name
	var alpha float64
	var alphab float64 = 0.2
	var delta float64 = 0.1
	var l int32 = 1
	var n int32 = 2
	st1 := make([]float64, metricNum)
	st2 := make([]float64, metricNum)
	at := make([]float64, metricNum)
	bt := make([]float64, metricNum)
	f := make([]float64, metricNum)
	e := make([]float64, metricNum)
	st1[0] = float64(metricList[0].CurrentValue.MilliValue())
	st2[0] = st1[0]
	at[0] = 2*st1[0] - st2[0]
	bt[0] = alpha / (1 - alpha) * (st1[0] - st2[0])
	f[0] = st2[0]
	e[0] = 0
	for i := 1; i < metricNum; i++ {
		f[i] = at[i-1] + bt[i-1]
		e[i] = f[i] - float64(metricList[i].CurrentValue.MilliValue())
		if e[i]*e[i-1] <= 0 {
			l = 1
		} else {
			l += 1
		}
		if l < n {
			alpha = alphab
		} else {
			alpha = math.Min(alpha+delta, 0.9)
		}
		st1[i] = alpha*float64(metricList[i].CurrentValue.MilliValue()) + (1-alpha)*st1[i-1]
		st2[i] = alpha*st1[i] + (1-alpha)*st2[i-1]
		at[i] = 2*st1[i] - st2[i]
		bt[i] = alpha / (1 - alpha) * (st1[i] - st2[i])
	}
	nextTotalValue := int64(math.Floor(float64(at[metricNum-1]+bt[metricNum-1]) + 0.5))
	if nextTotalValue < 0 {
		nextTotalValue = 0
	}
	nextTotalUtilization := int32(math.Floor(100*float64(nextTotalValue)/float64(totalRequest) + 0.5))
	var nextQuantity *resource.Quantity
	if metricName == corev1.ResourceCPU {
		nextQuantity = resource.NewMilliQuantity(nextTotalValue, resource.DecimalSI)
	}
	if metricName == corev1.ResourceMemory {
		nextQuantity = resource.NewMilliQuantity(nextTotalValue, resource.BinarySI)
	}
	nextMetricStatus := autoscalingv1.MetricStatus{
		Name:               metricName,
		CurrentValue:       nextQuantity,
		CurrentUtilization: nextTotalUtilization,
	}
	return &nextMetricStatus, nil
}

func removeMetricsForPods(metrics metricsclient.PodMetricsInfo, pods sets.String) {
	for pod := range pods {
		delete(metrics, pod)
	}
}

// calcUtilizationUsageRatio calculates the usage ratio of the target utilization
func calcUtilizationUsageRatio(metrics metricsclient.PodMetricsInfo, request int64, targetUtilization int32) (usageRatio float32, rawUtilization float32, usage int64) {
	for _, metric := range metrics {
		usage += metric.Value
	}
	totalRequest := request * int64(len(metrics))
	rawUtilization = 100 * float32(usage) / float32(totalRequest)
	usageRatio = rawUtilization / float32(targetUtilization)
	return usageRatio, rawUtilization, usage
}

// groupPods groups pods into ready, unready, missing and ignored pods.
// It returns the count of ready pods and the name string sets of pods belonging to different kinds.
// Unready pods refer to the pods in unready status.
// Missing pods refer to the pods without metric.
// Ignored pods are those which have been set a DeletionTimestamp or in a failed status.
func groupPods(podList []corev1.Pod, metrics metricsclient.PodMetricsInfo, resourceName corev1.ResourceName, cpuInitializationPeriod, delayOfInitialReadinessStatus time.Duration) (readyPodCount int32, unreadyPods, missingPods, ignoredPods sets.String) {
	unreadyPods = sets.NewString()
	missingPods = sets.NewString()
	ignoredPods = sets.NewString()
	for _, pod := range podList {
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodFailed {
			ignoredPods.Insert(pod.Name)
			continue
		}
		if pod.Status.Phase == corev1.PodPending {
			unreadyPods.Insert(pod.Name)
			continue
		}
		metric, metricFound := metrics[pod.Name]
		if !metricFound {
			missingPods.Insert(pod.Name)
			continue
		}
		if resourceName == corev1.ResourceCPU {
			unready := false
			var condition *corev1.PodCondition
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodReady {
					condition = &c
					break
				}
			}
			if condition == nil || pod.Status.StartTime == nil {
				unready = true
			} else {
				if pod.Status.StartTime.Add(cpuInitializationPeriod).After(time.Now()) {
					unready = condition.Status == corev1.ConditionFalse || metric.Timestamp.Before(condition.LastTransitionTime.Add(metric.Window))
				} else {
					unready = condition.Status == corev1.ConditionFalse && pod.Status.StartTime.Add(delayOfInitialReadinessStatus).After(condition.LastTransitionTime.Time)
				}
			}
			if unready {
				unreadyPods.Insert(pod.Name)
				continue
			}
		}
		readyPodCount++
	}
	return
}

// calcPodResourceRequest gets the resource request quantity specified by resourceName for each pod.
func calcPodResourceRequest(podSample *corev1.Pod, resourceName corev1.ResourceName) (int64, error) {
	podRequest := int64(0)
	for _, c := range podSample.Spec.Containers {
		if c.Resources.Requests == nil {
			return 0, fmt.Errorf("no resource request of the container: %v", c.Name)
		}
		if c.Resources.Limits == nil {
			return 0, fmt.Errorf("no resource limit of the container: %v", c.Name)
		}
		podRequest += getQuantityForTargetResource(c.Resources.Requests, resourceName).MilliValue()
	}
	return podRequest, nil
}

// calcCurrentResourceValue uses the metrics map to calculate the sum of the current value of the given pods
func calcCurrentResourceValue(metrics metricsclient.PodMetricsInfo, resourceName corev1.ResourceName) *resource.Quantity {
	totalValueInt64 := int64(0)
	for _, v := range metrics {
		totalValueInt64 += v.Value
	}
	var totalValueQuantity *resource.Quantity
	if resourceName == corev1.ResourceCPU {
		totalValueQuantity = resource.NewMilliQuantity(totalValueInt64, resource.DecimalSI)
	} else if resourceName == corev1.ResourceMemory {
		totalValueQuantity = resource.NewMilliQuantity(totalValueInt64, resource.BinarySI)
	}
	return totalValueQuantity
}

// calcCurrentResourceUtilization gets the total container resource request quantity (pod resource request quantity) and current utilization
// for the given pod resource. An err will be returned to indicate if any container resource request is missing.
func calcCurrentResourceUtilization(resourceValue *resource.Quantity, request int64, resourceName corev1.ResourceName) int32 {
	utilizationFloat := float64(resourceValue.MilliValue()) / float64(request)
	utilizationInt32 := int32(math.Floor(100*utilizationFloat + 0.5))
	return utilizationInt32
}

// getMetricsStatus transforms the metrics fetched from metricsclient into the format of autoscalingv1.MetricStatus,
// which container the name, value and utilization of pods resource.
// This first removes metrics from unready and ignored pods.
func (r *PredictiveHorizontalPodAutoscalerReconciler) getCurrentMetricStatus(pods []corev1.Pod, metrics metricsclient.PodMetricsInfo, request int64, resourceName corev1.ResourceName, mode autoscalingv1.ScaleMode, targetMetricSource *autoscalingv1.MetricSource) (currentMetricStatus *autoscalingv1.MetricStatus, metricLength int32) {
	// Copy PodMetricsInfo from metrics and remove unready and ignored pods.
	removedMetrics := make(metricsclient.PodMetricsInfo, len(metrics))
	for i, k := range metrics {
		removedMetrics[i] = k
	}
	_, unreadyPods, missingPods, ignoredPods := groupPods(pods, metrics, resourceName, r.CpuInitializationPeriod, r.DelayOfInitialReadinessStatus)
	removeMetricsForPods(removedMetrics, unreadyPods)
	removeMetricsForPods(removedMetrics, ignoredPods)

	var targetUtilization int32
	switch mode {
	case autoscalingv1.ScaleModeHorizontal:
		targetUtilization = targetMetricSource.UpperTargetUtilization
	case autoscalingv1.ScaleModeVertical:
		targetUtilization = targetMetricSource.LowerTargetUtilization
	}

	usageRatio, _, _ := calcUtilizationUsageRatio(metrics, request, targetUtilization)
	scaledUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0

	if len(missingPods) > 0 {
		if usageRatio < 1.0 {
			missingPodUtilization := int64(math.Max(100, float64(targetUtilization)))
			for podName := range missingPods {
				removedMetrics[podName] = metricsclient.PodMetric{Value: missingPodUtilization / 100 * request}
			}
		} else if usageRatio > 1.0 {
			for podName := range missingPods {
				removedMetrics[podName] = metricsclient.PodMetric{Value: 0}
			}
		}
	}
	if scaledUpWithUnready {
		for podName := range unreadyPods {
			removedMetrics[podName] = metricsclient.PodMetric{Value: 0}
		}
	}

	currentValue := calcCurrentResourceValue(removedMetrics, resourceName)
	currentUtilization := calcCurrentResourceUtilization(currentValue, request*int64(len(removedMetrics)), resourceName)
	return &autoscalingv1.MetricStatus{
		Name:               resourceName,
		CurrentValue:       currentValue,
		CurrentUtilization: currentUtilization,
	}, int32(len(removedMetrics))
}

// getDeploymentSelector convert the selector of appsv1.Deployment into labels.Selector
func getDeploymentSelector(deployment *appsv1.Deployment) *labels.Selector {
	matchLabels := deployment.Spec.Selector.MatchLabels
	matchExpressions := deployment.Spec.Selector.MatchExpressions
	selector := labels.NewSelector()
	for k, v := range matchLabels {
		require, _ := labels.NewRequirement(k, selection.In, []string{v})
		selector.Add(*require)
	}
	for _, v := range matchExpressions {
		operator := v.Operator
		var require *labels.Requirement
		switch operator {
		case metav1.LabelSelectorOpIn:
			{
				require, _ = labels.NewRequirement(v.Key, selection.In, v.Values)
			}
		case metav1.LabelSelectorOpNotIn:
			{
				require, _ = labels.NewRequirement(v.Key, selection.NotIn, v.Values)
			}
		case metav1.LabelSelectorOpExists:
			{
				require, _ = labels.NewRequirement(v.Key, selection.Exists, v.Values)
			}
		case metav1.LabelSelectorOpDoesNotExist:
			{
				require, _ = labels.NewRequirement(v.Key, selection.DoesNotExist, v.Values)
			}
		}
		selector.Add(*require)
	}
	return &selector
}

// constructPHPAMetricsList construct the new PredictiveHorizontalPodAutoscaler.Status.MetricsList
func constructPHPAMetricsList(phpa *autoscalingv1.PredictiveHorizontalPodAutoscaler, metricStatus *autoscalingv1.MetricStatus) {
	monitorWindow := phpa.Spec.MonitorWindowIntervalNum
	metricsList := phpa.Status.MetricsList
	if metricsList == nil {
		phpa.Status.MetricsList = make([]autoscalingv1.MetricStatus, 0)
	}
	metricsListLength := int32(len(metricsList))
	if metricsListLength < monitorWindow {
		phpa.Status.MetricsList = append(phpa.Status.MetricsList, *metricStatus)
	} else {
		metricsList := metricsList[1:monitorWindow:monitorWindow]
		phpa.Status.MetricsList = append(metricsList, *metricStatus)
	}
}

// Get quantity of target resource from resourceList.
func getQuantityForTargetResource(resourceList corev1.ResourceList, targetResource corev1.ResourceName) *resource.Quantity {
	if targetResource == corev1.ResourceCPU {
		return resourceList.Cpu()
	} else {
		return resourceList.Memory()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PredictiveHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.PredictiveHorizontalPodAutoscaler{}).
		Complete(r)
}
