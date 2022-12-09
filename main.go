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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	autoscalingv1 "myw.domain/autoscaling/api/v1"
	"myw.domain/autoscaling/controllers"
	//+kubebuilder:scaffold:imports
)

const (
	DefaultSyncPeriod                            = time.Second * 10
	SyncRatio                            float32 = 0.9
	DefaultCPUInitializationPeriod               = time.Minute * 5
	DefaultDelayOfInitialReadinessStatus         = time.Second * 30
	DefaultTolerance                     float32 = 0.1
	DefaultScaleHistoryLimit             int32   = 20
)

const (
	targetMiddleGV   = "apps/v1"
	targetMiddleKind = "ReplicaSet"
	targetGV         = "apps/v1"
	targetKind       = "Deployment"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,sideEffects=None,admissionReviewVersions=v1
type PodResourceAllocator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (ra *PodResourceAllocator) Handle(ctx context.Context, req admission.Request) admission.Response {
	fmt.Println("webhook handle")
	pod := &corev1.Pod{}

	err := ra.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// List phpa that matches the target deployment(replicaset/pod).
	phpa := &autoscalingv1.PredictiveHorizontalPodAutoscaler{}
	phpaList := &autoscalingv1.PredictiveHorizontalPodAutoscalerList{}
	for _, ownerReference := range pod.GetOwnerReferences() {
		if ownerReference.APIVersion == targetMiddleGV && ownerReference.Kind == targetMiddleKind {
			rsNamespacedName := types.NamespacedName{
				Namespace: req.Namespace,
				Name:      ownerReference.Name,
			}
			replicaSet := &appsv1.ReplicaSet{}
			err := ra.Client.Get(ctx, rsNamespacedName, replicaSet)
			fmt.Printf("查询到replicaset：%v\n", replicaSet)
			if err == nil {
				key := "target-reference"
				val := req.Namespace + "-" + replicaSet.OwnerReferences[0].Name
				require, _ := labels.NewRequirement(key, selection.In, []string{val})
				selector := labels.NewSelector().Add(*require)
				err := ra.Client.List(ctx, phpaList, &client.ListOptions{LabelSelector: selector})
				if err == nil && len(phpaList.Items) != 0 {
					fmt.Printf("完成phpa和replicaset的匹配\n")
					*phpa = phpaList.Items[0]
					break
				}
			}
		}
	}

	// If corresponding phpa found, mutate the pod Spec.
	if len(phpaList.Items) > 0 {
		for i, c := range pod.Spec.Containers {
			pod.Spec.Containers[i].Resources = phpa.Status.DesiredResourceRequirements[c.Name]
		}
	}
	fmt.Printf("最终pod：%v\n", pod)

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (ra *PodResourceAllocator) InjectDecoder(d *admission.Decoder) error {
	ra.decoder = d
	return nil
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(autoscalingv1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme

	//DefaultRESTMapper used to get mappings from GVK to GVR
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	syncPeriod := DefaultSyncPeriod
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9d05e851.myw.domain",
		SyncPeriod:             &syncPeriod,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	monitorInterval := time.Duration(int64(float64(syncPeriod.Nanoseconds()) * float64(SyncRatio)))
	if err = (&controllers.PredictiveHorizontalPodAutoscalerReconciler{
		Config:                        ctrl.GetConfigOrDie(),
		Client:                        mgr.GetClient(),
		Scheme:                        mgr.GetScheme(),
		MonitorInterval:               monitorInterval,
		CpuInitializationPeriod:       DefaultCPUInitializationPeriod,
		DelayOfInitialReadinessStatus: DefaultDelayOfInitialReadinessStatus,
		Tolerance:                     DefaultTolerance,
		ScaleHistoryLimit:             DefaultScaleHistoryLimit,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PredictiveHorizontalPodAutoscaler")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Register the webhook path with the corresponding handler into the webhook server.
	mgr.GetWebhookServer().Register("/mutate-v1-pod", &webhook.Admission{Handler: &PodResourceAllocator{Client: mgr.GetClient()}})

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
