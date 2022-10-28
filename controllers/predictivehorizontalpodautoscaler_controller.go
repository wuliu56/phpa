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
	"strconv"
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
	Scheme                        *runtime.Scheme
	MonitorInterval               time.Duration
	cpuInitializationPeriod       time.Duration
	delayOfInitialReadinessStatus time.Duration
	tolerance                     float32
	scaleHistoryLimit             int32
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
	//get the PredictiveHorizontalPodAutoscaler
	log.V(1).Info("fetching PredictiveHorizontalPodAutoscaler")
	var phpa autoscalingv1.PredictiveHorizontalPodAutoscaler
	if err := r.Get(ctx, req.NamespacedName, &phpa); err != nil {
		log.Error(err, "unable to fetch PredictiveHorizontalPodAutoscaler")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	//fetch fields of PredictiveHorizontalPodAutoscaler
	spec := phpa.Spec.DeepCopy()
	status := phpa.Status.DeepCopy()
	//alpha := spec.Alpha
	maxReplicas := spec.MaxReplicas
	minReplicas := spec.MinReplicas
	//monitorWindowIntervalNum := spec.MonitorWindowIntervalNum
	scaleDownStabilizationWindowSeconds := spec.ScaleDownStabilizationWindowSeconds
	//scaleHistoryLimit := spec.ScaleHistoryLimit
	scaleTargetRef := spec.ScaleTargetRef
	targetMetricSource := spec.Metrics
	//currentReplicas := status.CurrentReplicas
	//desiredReplicas := status.DesiredReplicas
	lastScaleTime := status.LastScaleTime
	//metricsList := status.MetricsList

	//get the selector of the given workload(deployment/statefulset) to list the pods
	/*reference := fmt.Sprintf("%v/%v/%v", scaleTargetRef.Kind, req.Namespace, scaleTargetRef.Name)
	fmt.Printf("reference: %v \n", reference)
	targetGV, err := schema.ParseGroupVersion(scaleTargetRef.APIVersion)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid API version in scale target reference: %v", err)
	}
	targetGK := schema.GroupKind{
		Group: targetGV.Group,
		Kind:  scaleTargetRef.Kind,
	}
	fmt.Printf("GVK: %v/%v \n", targetGK.Group, targetGK.Kind)

	gvkmap := r.Scheme.AllKnownTypes()
	gvlist := []schema.GroupVersion{}
	for gvk := range gvkmap {
		gvlist = append(gvlist, gvk.GroupVersion())
	}
	for i, v := range gvlist {
		fmt.Printf("gv %v: %v\n", i, v)
	}
	defaultRESTMapper := apimeta.NewDefaultRESTMapper(gvlist)
	mappings, err := defaultRESTMapper.RESTMappings(targetGK)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to determine resource for scale target reference : %v", err)
	}
	var scale k8sautoscalingv1.Scale
	for _, mapping := range mappings {
		targetGR := mapping.Resource.GroupResource()
		r.Get(ctx, req.NamespacedName, &scale, )

	}
	*/

	//monitorInterval is used to control the rate that the phpa status is updated and the replica count of workload spec is changed
	monitorInterval := time.Duration(int64(float32(r.MonitorInterval.Nanoseconds()) * 0.9))
	lastMonitorTime := status.LastMonitorTime
	if lastMonitorTime != nil {
		if lastMonitorTime.Time.Add(monitorInterval).After(time.Now()) {
			log.V(1).Info("too short interval to reconcile")
			return ctrl.Result{}, nil
		}
	}

	//initialize cpuInitializationPeriod, delayOfInitialReadinessStatus, tolerance and scaleHistoryLimit
	if spec.CpuInitializationPeriod != nil {
		var err error
		r.cpuInitializationPeriod, err = time.ParseDuration(*spec.CpuInitializationPeriod)
		if err != nil {
			log.Error(err, "unable to parse CpuInitializationPeriod in the format of time.Duration")
			return ctrl.Result{}, err
		}
	} else {
		r.cpuInitializationPeriod, _ = time.ParseDuration("5m")
	}

	if spec.DelayOfInitialReadinessStatus != nil {
		var err error
		r.delayOfInitialReadinessStatus, err = time.ParseDuration(*spec.DelayOfInitialReadinessStatus)
		if err != nil {
			log.Error(err, "unable to parse DelayOfInitialReadinessStatus in the format of time.Duration")
			return ctrl.Result{}, err
		}
	} else {
		r.delayOfInitialReadinessStatus, _ = time.ParseDuration("30s")
	}

	if spec.Tolerance != nil {
		tolerance64, err := strconv.ParseFloat(*spec.Tolerance, 32)
		if err != nil {
			log.Error(err, "unable to parse Tolerance in the format of float32")
			return ctrl.Result{}, err
		}
		r.tolerance = float32(tolerance64)
	} else {
		r.tolerance = float32(0.1)
	}
	r.scaleHistoryLimit = *spec.ScaleHistoryLimit

	// get the resource for the target reference
	log.V(1).Info("determining resource for scale target reference")
	var deployment appsv1.Deployment
	namespace := req.Namespace
	deploymentName := scaleTargetRef.Name
	targetGVR := "apps/v1/deployment"
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      deploymentName,
	}, &deployment); err != nil {
		log.Error(err, "unable to determine resource for scale target reference", "targetGVR", targetGVR, "namespace", namespace, "deploymentName", deploymentName)
		return ctrl.Result{}, err
	}

	//list the pods for the target resource
	var podList corev1.PodList
	selector := getMetricSelector(&deployment)
	if err := r.List(ctx, &podList, client.InNamespace(req.Namespace), client.MatchingLabelsSelector{Selector: *selector}); err != nil {
		log.Error(err, "unable to list the pods for target resource", "targetGVR", targetGVR, "namespace", namespace, "deploymentName", deploymentName)
		return ctrl.Result{}, err
	}

	//construct metricsClient and fetch raw pod metrics
	resourceClient := v1beta1.NewForConfigOrDie(r.Config)
	metricsClient := metricsclient.NewRESTMetricsClient(
		resourceClient,
		nil,
		nil,
	)
	resourceName := spec.Metrics.Name
	log.V(1).Info("fetching the current raw resource metrics of scale target from metricsClient")
	metrics, _, err := metricsClient.GetResourceMetric(ctx, resourceName, namespace, *selector, "")
	if err != nil {
		log.Error(err, "unable to fetch the current raw resource metrics of scale target from metricsClient")
	}
	//calculate desired replicas based on both the predicted next metric and the current metric
	//calculate desired replicas based on the current metric
	currentReplicas := deployment.Status.Replicas
	var currentDesiredReplicas int32
	var currentUsage int64
	request, err := getPodResourceRequest(&deployment, resourceName)
	if err != nil {
		log.Error(err, "unable to get pod resource request")
		return ctrl.Result{}, err
	}
	currentDesiredReplicas, currentUsage = r.calcDesiredReplicas(podList.Items, metrics, currentReplicas, request, phpa.Spec.Metrics)

	//construct the new metric status list and predict the next metric
	totalRequest := request * int64(len(metrics))
	fmt.Printf("currentDesiredReplicas: %v, currentUsage: %v\n", currentDesiredReplicas, currentUsage)
	metricStatus := getCurrentMetricStatus(metrics, request, resourceName)
	constructPHPAMetricsList(&phpa, metricStatus)
	nextMetricStatus, err := predictNextMetricStatus(&phpa, totalRequest)
	if err != nil {
		log.Error(err, "failed to predict next metric status")
		return ctrl.Result{}, err
	}

	//calculate desired replicas based on the predicted metric
	fmt.Printf("variance: %v\n", calcStandardDeviation(phpa.Status.MetricsList))
	predictedDesiredReplicas := r.predictDesiredReplicas(nextMetricStatus, currentReplicas, int32(len(metrics)), targetMetricSource)
	fmt.Printf("predictedDesiredReplicas: %v, predictedUsage: %v\n", predictedDesiredReplicas, nextMetricStatus.CurrentValue)

	//decide the final desired replicas based on both the current and predicted metrics
	var finalDesiredReplicas int32
	if currentDesiredReplicas == currentReplicas {
		finalDesiredReplicas = currentReplicas
	} else if currentDesiredReplicas > currentReplicas {
		if predictedDesiredReplicas > currentDesiredReplicas {
			finalDesiredReplicas = predictedDesiredReplicas
		} else {
			finalDesiredReplicas = currentDesiredReplicas
		}
	} else {
		finalDesiredReplicas = currentDesiredReplicas
	}

	//refactor the final desired replicas based on the minReplicas and maxReplicas
	if finalDesiredReplicas < *minReplicas {
		finalDesiredReplicas = *minReplicas
	} else if finalDesiredReplicas > *maxReplicas {
		finalDesiredReplicas = *maxReplicas
	}
	phpa.Status.CurrentReplicas = &currentReplicas
	phpa.Status.DesiredReplicas = &finalDesiredReplicas

	//do the scaling and record the scale event
	if finalDesiredReplicas == currentReplicas {
		log.V(0).Info("not to scale because of the same desired replicas with actual replicas")
	} else if finalDesiredReplicas > currentReplicas {
		log.V(0).Info("scale up", "current replicas", currentReplicas, "desired replicas", finalDesiredReplicas)
		deployment.Spec.Replicas = &finalDesiredReplicas
		if err := r.Update(ctx, &deployment); err != nil {
			log.Error(err, "failed to scale pod replicas")
			return ctrl.Result{}, err
		}
		nowTime := metav1.Time{Time: time.Now()}
		phpa.Status.LastScaleTime = &nowTime
		newScaleEvent := autoscalingv1.ScaleEvent{
			Time:     &nowTime,
			Replicas: &finalDesiredReplicas,
		}
		r.recordScaleEvent(&phpa, newScaleEvent)
	} else {
		scaleDownStabilizationWindowDuration := time.Duration(*scaleDownStabilizationWindowSeconds) * time.Second
		var ifScale bool
		if lastScaleTime != nil {
			ifScale = time.Now().After(lastScaleTime.Time.Add(scaleDownStabilizationWindowDuration))
		} else {
			ifScale = true
		}
		if ifScale {
			log.V(0).Info("scale down", "current replicas", currentReplicas, "desired replicas", finalDesiredReplicas)
			deployment.Spec.Replicas = &finalDesiredReplicas
			if err := r.Update(ctx, &deployment); err != nil {
				log.Error(err, "failed to scale pod replicas")
				return ctrl.Result{}, err
			}
			nowTime := metav1.Time{Time: time.Now()}
			phpa.Status.LastScaleTime = &nowTime
			newScaleEvent := autoscalingv1.ScaleEvent{
				Time:     &nowTime,
				Replicas: &finalDesiredReplicas,
			}
			r.recordScaleEvent(&phpa, newScaleEvent)
		} else {
			log.V(0).Info("not to scale down because last scaling is too close, within scaleDownStabilizationWindow")
		}
	}

	//update the phpa status
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

// calcDesiredReplicas calculates the desired replicas with the current metric for the target VALUE or UTILIZATION
func (r *PredictiveHorizontalPodAutoscalerReconciler) calcDesiredReplicas(podList []corev1.Pod, metrics metricsclient.PodMetricsInfo, currentReplicas int32, request int64, targetMetricSource *autoscalingv1.MetricSource) (desiredReplicas int32, usage int64) {
	resourceName := targetMetricSource.Name
	targetAverageValue := targetMetricSource.TargetAverageValue
	targetAverageUtilization := targetMetricSource.TargetAverageUtilization
	readyPodCount, unreadyPods, missingPods, ignoredPods := groupPods(podList, metrics, resourceName, r.cpuInitializationPeriod, r.delayOfInitialReadinessStatus)
	removeMetricsForPods(metrics, unreadyPods)
	removeMetricsForPods(metrics, ignoredPods)

	if targetAverageUtilization == nil {
		//calculate the desired replicas for target VALUE
		usageRatio, usage := calcValueUsageRatio(metrics, targetAverageValue.MilliValue())
		scaledUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0
		if !scaledUpWithUnready && len(missingPods) == 0 {
			if math.Abs(float64(usageRatio-1.0)) <= float64(r.tolerance) {
				// return the current replicas if the change would be too small
				return currentReplicas, usage
			}
			// if there's no unready or missing pods, we can calculate the new replica count now
			return int32(math.Ceil(float64(usageRatio) * float64(readyPodCount))), usage
		}

		// For the missing pods, we assume their usage based on the scale direction.
		// When it's to scale up, the usage value is assumed to be 0.
		// When it's to scale down, the usage value is assumed to be the same as target average value.
		// This helps dampen the magnitude of any potential scale
		if len(missingPods) > 0 {
			if usageRatio > 1.0 {
				for podName := range missingPods {
					metrics[podName] = metricsclient.PodMetric{Value: 0}
				}
			} else {
				for podName := range missingPods {
					metrics[podName] = metricsclient.PodMetric{Value: targetAverageValue.MilliValue()}
				}
			}
		}

		//For unready pods, we assume they consume 0 resource in case of a scale up.
		if scaledUpWithUnready {
			for podName := range metrics {
				metrics[podName] = metricsclient.PodMetric{Value: 0}
			}
		}

		//re-run the usage calculation
		newUsageRatio, _ := calcValueUsageRatio(metrics, targetAverageValue.MilliValue())

		if math.Abs(float64(newUsageRatio-1.0)) <= float64(r.tolerance) || (usageRatio > 1.0 && newUsageRatio < 1.0) || (usageRatio < 1.0 && newUsageRatio > 1.0) {
			// return the current replicas if the change would be too small,
			// or if the new usage ratio would cause a change in scale direction
			return currentReplicas, usage
		}

		newReplicas := int32(math.Ceil(float64(newUsageRatio) * float64(len(metrics))))
		if (newUsageRatio < 1.0 && newReplicas > currentReplicas) || (newUsageRatio > 1.0 && newReplicas < currentReplicas) {
			// return the current replicas if the change of metrics length would cause a change in scale direction
			return currentReplicas, usage
		}
		return newReplicas, usage
	} else {
		//calculate the desired replicas for target UTILIZATION
		usageRatio, _, usage := calcUtilizationUsageRatio(metrics, request, *targetAverageUtilization)

		scaledUpWithUnready := len(unreadyPods) > 0 && usageRatio > 1.0
		if !scaledUpWithUnready && len(missingPods) == 0 {
			if math.Abs(float64(usageRatio)-1.0) <= float64(r.tolerance) {
				return currentReplicas, usage
			}
			return int32(math.Ceil(float64(usageRatio) * float64(readyPodCount))), usage
		}

		if len(missingPods) > 0 {
			if usageRatio < 1.0 {
				missingPodUtilization := int64(math.Max(100, float64(*targetAverageUtilization)))
				for podName := range missingPods {
					metrics[podName] = metricsclient.PodMetric{Value: missingPodUtilization / 100 * request}
				}
			} else if usageRatio > 1.0 {
				for podName := range missingPods {
					metrics[podName] = metricsclient.PodMetric{Value: 0}
				}
			}
		}

		if scaledUpWithUnready {
			for podName := range unreadyPods {
				metrics[podName] = metricsclient.PodMetric{Value: 0}
			}
		}

		newUsageRatio, _, _ := calcUtilizationUsageRatio(metrics, request, *targetAverageUtilization)

		if math.Abs(float64(newUsageRatio)-1.0) <= float64(r.tolerance) || (newUsageRatio > 1.0 && usageRatio < 1.0) || (newUsageRatio < 1.0 && usageRatio > 1.0) {
			return currentReplicas, usage
		}

		newReplicas := int32(math.Ceil(float64(newUsageRatio) * float64(len(metrics))))
		if newUsageRatio < 1.0 && newReplicas > currentReplicas || (newUsageRatio > 1.0 && newReplicas < currentReplicas) {
			return currentReplicas, usage
		}
		return newReplicas, usage
	}
}

// predictDesiredReplicas calculates the desired replicas with the predicted metric for the target VALUE or UTILIZATION
func (r *PredictiveHorizontalPodAutoscalerReconciler) predictDesiredReplicas(nextMetricStatus *autoscalingv1.MetricStatus, currentReplicas int32, metricLength int32, targetMetricSource *autoscalingv1.MetricSource) (desiredReplicas int32) {
	targetAverageValue := targetMetricSource.TargetAverageValue
	targetAverageUtilization := targetMetricSource.TargetAverageUtilization
	nextValue := nextMetricStatus.CurrentValue
	nextUtilization := nextMetricStatus.CurrentUtilization
	if targetAverageUtilization == nil {
		//predict the desired replicas for the target VALUE
		nextUsageRatio := float64(nextValue.MilliValue()) / float64(targetAverageValue.MilliValue()) / float64(metricLength)
		desiredReplicas = int32(math.Ceil(nextUsageRatio * float64(metricLength)))
		if math.Abs(nextUsageRatio-1.0) <= float64(r.tolerance) {
			return currentReplicas
		}
		return desiredReplicas
	} else {
		//predict the desired replicas for the target UTILIZATION
		nextUsageRatio := float64(*nextUtilization) / float64(*targetAverageUtilization)
		desiredReplicas = int32(math.Ceil(nextUsageRatio * float64(metricLength)))
		if math.Abs(nextUsageRatio-1.0) <= float64(r.tolerance) {
			return currentReplicas
		}
		return desiredReplicas
	}
}

// recordScaleEvent records a scale event.
func (r *PredictiveHorizontalPodAutoscalerReconciler) recordScaleEvent(phpa *autoscalingv1.PredictiveHorizontalPodAutoscaler, scaleEvent autoscalingv1.ScaleEvent) {
	scaleEventsList := phpa.Status.ScaleEventsList
	if scaleEventsList == nil {
		phpa.Status.ScaleEventsList = make([]autoscalingv1.ScaleEvent, 0)
	}
	scaleEventNum := int32(len(scaleEventsList))
	if scaleEventNum < r.scaleHistoryLimit {
		phpa.Status.ScaleEventsList = append(scaleEventsList, scaleEvent)
	} else {
		scaleEventsList = scaleEventsList[1:r.scaleHistoryLimit:r.scaleHistoryLimit]
		phpa.Status.ScaleEventsList = append(scaleEventsList, scaleEvent)
	}
}

// predictNextMetricStatus predicts the value and utilization at the next time interval,
// and returns *autoscalingv1.MetricStatus composed of the information
func predictNextMetricStatus(phpa *autoscalingv1.PredictiveHorizontalPodAutoscaler, request int64) (*autoscalingv1.MetricStatus, error) {
	metricList := phpa.Status.MetricsList
	metricNum := len(phpa.Status.MetricsList)
	metricName := metricList[0].Name
	var alpha float32
	if len(metricList) == 0 {
		return nil, fmt.Errorf("there's no metric provided for prediction")
	}
	if phpa.Spec.Alpha != nil {
		alpha64, err := strconv.ParseFloat(*phpa.Spec.Alpha, 32)
		if err != nil {
			return nil, fmt.Errorf("incorrect format of alpha (%v) provided : %v", phpa.Spec.Alpha, err)
		}
		alpha = float32(alpha64)
	} else {
		alpha = calcAlphaForMetrics(metricList)
	}
	st1 := make([]float32, metricNum)
	st2 := make([]float32, metricNum)
	at := make([]float32, metricNum)
	bt := make([]float32, metricNum)
	st1[0] = float32(metricList[0].CurrentValue.MilliValue())
	st2[0] = st1[0]
	for i := 1; i < metricNum; i++ {
		st1[i] = alpha*float32(metricList[i].CurrentValue.MilliValue()) + (float32(1)-alpha)*st1[i-1]
		st2[i] = alpha*st1[i] + (float32(1)-alpha)*st2[i-1]
	}
	for i := 0; i < metricNum; i++ {
		at[i] = 2*st1[i] - st2[i]
		bt[i] = alpha / (1 - alpha) * (st1[i] - st2[i])
	}
	nextTotalValue := int64(math.Floor(float64(at[metricNum-1]+bt[metricNum-1]) + 0.5))
	if nextTotalValue < 0 {
		nextTotalValue = 0
	}
	nextTotalUtilization := int32(math.Floor(100*float64(nextTotalValue)/float64(request) + 0.5))
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
		CurrentUtilization: &nextTotalUtilization,
	}
	return &nextMetricStatus, nil
}

func removeMetricsForPods(metrics metricsclient.PodMetricsInfo, pods sets.String) {
	for pod := range pods {
		delete(metrics, pod)
	}
}

// calcValueUsageRatio calculates the usage ratio of the target value
func calcValueUsageRatio(metrics metricsclient.PodMetricsInfo, targetUsage int64) (usageRatio float32, usage int64) {
	usage = int64(0)
	for _, metric := range metrics {
		usage += metric.Value
	}
	targetUsage = targetUsage * int64(len(metrics))
	usageRatio = float32(usage) / float32(targetUsage)
	return usageRatio, usage
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

// getPodResourceRequest gets the resource request quantity specified by resourceName for each pod of the deployment
func getPodResourceRequest(deployment *appsv1.Deployment, resourceName corev1.ResourceName) (int64, error) {
	totalRequest := int64(0)
	if resourceName == corev1.ResourceCPU {
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Resources.Requests == nil {
				return 0, fmt.Errorf("no resource request for cpu of the container: %v", c.Name)
			}
			request := c.Resources.Requests.Cpu().MilliValue()
			totalRequest += int64(request)
		}
	}
	if resourceName == corev1.ResourceMemory {
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Resources.Requests == nil {
				return 0, fmt.Errorf("no resource request for memory of the container: %v", c.Name)
			}
			request := c.Resources.Requests.Memory().MilliValue()
			totalRequest += int64(request)
		}
	}
	return totalRequest, nil
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
func getCurrentMetricStatus(metrics metricsclient.PodMetricsInfo, request int64, resourceName corev1.ResourceName) *autoscalingv1.MetricStatus {
	currentValue := calcCurrentResourceValue(metrics, resourceName)
	currentUtilization := calcCurrentResourceUtilization(currentValue, request*int64(len(metrics)), resourceName)
	return &autoscalingv1.MetricStatus{
		Name:               resourceName,
		CurrentValue:       currentValue,
		CurrentUtilization: &currentUtilization,
	}
}

// getMetricSelector convert the selector of appsv1.Deployment into labels.Selector
func getMetricSelector(deployment *appsv1.Deployment) *labels.Selector {
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
	if metricsListLength < *monitorWindow {
		phpa.Status.MetricsList = append(phpa.Status.MetricsList, *metricStatus)
	} else {
		metricsList := metricsList[1:*monitorWindow:*monitorWindow]
		phpa.Status.MetricsList = append(metricsList, *metricStatus)
	}
}

// calcAlphaForMetrics calculates the alpha applied to resource prediction based on the avsd
func calcAlphaForMetrics(metrics []autoscalingv1.MetricStatus) float32 {
	alpha := float32(0.3)
	return alpha
}

// calcStandardDeviation calculates the standard deviation of the given slice
func calcStandardDeviation(metrics []autoscalingv1.MetricStatus) int64 {
	var variance float64
	var sum float64
	milliFlag := true
	for _, metric := range metrics {
		if metric.Name == corev1.ResourceMemory {
			milliFlag = false
		}
	}
	if milliFlag {
		for _, metric := range metrics {
			sum += float64(metric.CurrentValue.MilliValue())
		}
		mean := float64(sum) / float64(len(metrics))
		for _, metric := range metrics {
			variance += math.Pow(float64(metric.CurrentValue.MilliValue())-mean, 2)
		}
		return int64(math.Sqrt(math.Floor(variance + 0.5)))
	}
	for _, metric := range metrics {
		sum += float64(metric.CurrentValue.Value())
	}
	mean := float64(sum) / float64(len(metrics))
	for _, metric := range metrics {
		variance += math.Pow(float64(metric.CurrentValue.Value())-mean, 2)
	}
	return int64(math.Sqrt(math.Floor(variance + 0.5)))
}

// SetupWithManager sets up the controller with the Manager.
func (r *PredictiveHorizontalPodAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.PredictiveHorizontalPodAutoscaler{}).
		Complete(r)
}
