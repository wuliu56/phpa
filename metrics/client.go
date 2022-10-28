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

package metrics

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	resourceclient "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	customclient "k8s.io/metrics/pkg/client/custom_metrics"
	externalclient "k8s.io/metrics/pkg/client/external_metrics"
)

// MetricsClient defines methods to provide all kinds of metrics.
type MetricsClient interface {
	// GetResourceMetric gets the given resource metric and an associated oldest timestamp
	// for the specified named container in all pods matching the specified selector in the given namespace
	// and when the container is an empty string it returns the sum of all the container metrics.
	GetResourceMetric(ctx context.Context, resource v1.ResourceName, namespace string, selector labels.Selector, container string) (PodMetricsInfo, metav1.Time, error)

	//ADD NEW METHODS HERE TO FETCH METRICS FROM OTHER APIS:
}

// NewRESTMetricsClient is a factory function.
// It returns a MetricsClient interface which defines methods to provide all kinds of metrics.
func NewRESTMetricsClient(resourceClient resourceclient.PodMetricsesGetter, customClient customclient.CustomMetricsClient, externalClient externalclient.ExternalMetricsClient) MetricsClient {
	return &restMetricsClient{
		&resourceMetricsClient{resourceClient},
		&customMetricsClient{customClient},
		&externalMetricsClient{externalClient},
	}
}

// restMetricsClient is a client that fetches metrics from resource metrics API, custom metrics API and external metrics API.
type restMetricsClient struct {
	*resourceMetricsClient
	*customMetricsClient
	*externalMetricsClient
}

// resourceMetricsClient fetches data from the resource metrics API.
type resourceMetricsClient struct {
	client resourceclient.PodMetricsesGetter
}

// customMetricsClient fetches data from the custom metrics API.
type customMetricsClient struct {
	client customclient.CustomMetricsClient
}

// externalMetricsClient fetches data from the external metrics API.
type externalMetricsClient struct {
	client externalclient.ExternalMetricsClient
}

// PodMetric contains pod metric value.
type PodMetric struct {
	Timestamp time.Time
	Window    time.Duration
	Value     int64
}

// PodMetricsInfo contains pod metrics as a map from pod names to PodMetric
type PodMetricsInfo map[string]PodMetric

// GetResourceMetric gets the given resource metric (and an associated oldest timestamp)
// for all pods matching the specified selector in the given namespace
func (c *resourceMetricsClient) GetResourceMetric(ctx context.Context, resource v1.ResourceName, namespace string, selector labels.Selector, container string) (PodMetricsInfo, metav1.Time, error) {
	metrics, err := c.client.PodMetricses(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, metav1.Time{}, fmt.Errorf("unable to fetch metrics from resource metrics API: %v", err)
	}

	if len(metrics.Items) == 0 {
		return nil, metav1.Time{}, fmt.Errorf("no metrics returned from resource metrics API")
	}

	var res PodMetricsInfo
	if container != "" {
		res, err = getContainerMetrics(metrics.Items, resource, container)
		if err != nil {
			return nil, metav1.Time{}, fmt.Errorf("failed to get container metrics: %v", err)
		}
	} else {
		res = getPodMetrics(metrics.Items, resource)
	}
	timestamp := metrics.Items[0].Timestamp
	return res, timestamp, nil
}

func getContainerMetrics(rawMetrics []metricsapi.PodMetrics, resource v1.ResourceName, container string) (PodMetricsInfo, error) {
	res := make(PodMetricsInfo, len(rawMetrics))
	for _, m := range rawMetrics {
		containerFound := false
		for _, c := range m.Containers {
			if c.Name == container {
				containerFound = true
				if val, resFound := c.Usage[resource]; resFound {
					res[m.Name] = PodMetric{
						Timestamp: m.Timestamp.Time,
						Window:    m.Window.Duration,
						Value:     val.MilliValue(),
					}
				}
				break
			}
		}
		if !containerFound {
			return nil, fmt.Errorf("container %s not present in metrics for pod %s/%s", container, m.Namespace, m.Name)
		}
	}
	return res, nil
}

func getPodMetrics(rawMetrics []metricsapi.PodMetrics, resource v1.ResourceName) PodMetricsInfo {
	res := make(PodMetricsInfo, len(rawMetrics))
	for _, m := range rawMetrics {
		missing := len(m.Containers) == 0
		podSum := int64(0)
		for _, c := range m.Containers {
			val, resFound := c.Usage[resource]
			if !resFound {
				missing = true
				break
			}
			podSum += val.MilliValue()
		}
		if !missing {
			res[m.Name] = PodMetric{
				Timestamp: m.Timestamp.Time,
				Window:    m.Window.Duration,
				Value:     podSum,
			}
		}
	}
	return res
}
