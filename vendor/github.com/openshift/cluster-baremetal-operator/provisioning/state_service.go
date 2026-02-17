package provisioning

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
)

const (
	stateService             = "metal3-state"
	httpPortName             = "http"
	vmediaHttpsPortName      = "vmedia-https"
	metricsPortName          = "metrics"
	ironicPrometheusRuleName = "metal3-defaults"
)

func newMetal3StateService(info *ProvisioningInfo) *corev1.Service {
	port, _ := strconv.Atoi(baremetalHttpPort)             // #nosec
	httpsPort, _ := strconv.Atoi(baremetalVmediaHttpsPort) // #nosec
	ironicPort := getControlPlanePort(info)

	ports := []corev1.ServicePort{
		{
			Name: "ironic",
			Port: int32(ironicPort),
		},
		{
			Name: httpPortName,
			Port: int32(port),
		},
	}
	// Always expose port 6385 since it's always available as a hostPort
	// either directly from the main pod or via the ironic-proxy DaemonSet.
	// When ironic-proxy is enabled (ironicPort == 6388), the metal3 pod listens
	// on port 6388, so we need to set targetPort to route traffic correctly.
	if ironicPort != baremetalIronicPort {
		ports = append(ports, corev1.ServicePort{
			Name:       "ironic-api",
			Port:       int32(baremetalIronicPort),
			TargetPort: intstr.FromInt32(int32(ironicPort)),
		})
	}
	if !info.ProvConfig.Spec.DisableVirtualMediaTLS {
		ports = append(ports, corev1.ServicePort{
			Name: vmediaHttpsPortName,
			Port: int32(httpsPort),
		})
	}
	if info.ProvConfig.Spec.PrometheusExporter != nil && info.ProvConfig.Spec.PrometheusExporter.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: metricsPortName,
			Port: int32(baremetalMetricsPort),
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stateService,
			Namespace: info.Namespace,
			Labels: map[string]string{
				cboLabelName: stateService,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				cboLabelName: stateService,
			},
			Ports: ports,
		},
	}
}

func EnsureMetal3StateService(info *ProvisioningInfo) (updated bool, err error) {
	metal3StateService := newMetal3StateService(info)

	err = controllerutil.SetControllerReference(info.ProvConfig, metal3StateService, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on service: %w", err)
		return
	}

	_, updated, err = resourceapply.ApplyService(context.Background(),
		info.Client.CoreV1(), info.EventRecorder, metal3StateService)
	if err != nil {
		err = fmt.Errorf("unable to apply Metal3-state service: %w", err)
	}
	return
}

func DeleteMetal3StateService(info *ProvisioningInfo) error {
	return client.IgnoreNotFound(info.Client.CoreV1().Services(info.Namespace).Delete(context.Background(), stateService, metav1.DeleteOptions{}))
}

// NewIronicServiceMonitor creates a ServiceMonitor for Ironic metrics
func NewIronicServiceMonitor(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "ServiceMonitor",
			"metadata": map[string]interface{}{
				"name":      ironicPrometheusExporterName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					cboLabelName: stateService,
				},
			},
			"spec": map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"port": metricsPortName,
					},
				},
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						cboLabelName: stateService,
					},
				},
			},
		},
	}
}

// EnsureIronicServiceMonitor ensures the ServiceMonitor exists when sensor metrics are enabled
func EnsureIronicServiceMonitor(info *ProvisioningInfo) (bool, error) {
	ctx := context.Background()

	// If metrics are disabled, ensure ServiceMonitor is deleted
	if info.ProvConfig.Spec.PrometheusExporter == nil || !info.ProvConfig.Spec.PrometheusExporter.Enabled {
		return false, DeleteIronicServiceMonitor(info)
	}

	serviceMonitor := NewIronicServiceMonitor(info.Namespace)
	if err := controllerutil.SetControllerReference(info.ProvConfig, serviceMonitor, info.Scheme); err != nil {
		return false, fmt.Errorf("unable to set controllerReference on ServiceMonitor: %w", err)
	}

	// Apply or Update
	_, updated, err := resourceapply.ApplyServiceMonitor(ctx, info.DynamicClient, info.EventRecorder, serviceMonitor)
	if err != nil {
		return false, fmt.Errorf("failed to apply ServiceMonitor: %w", err)
	}

	return updated, nil
}

// DeleteIronicServiceMonitor deletes the ServiceMonitor
func DeleteIronicServiceMonitor(info *ProvisioningInfo) error {
	serviceMonitor := NewIronicServiceMonitor(info.Namespace)
	_, _, err := resourceapply.DeleteServiceMonitor(context.Background(), info.DynamicClient, info.EventRecorder, serviceMonitor)
	return err
}

// NewIronicPrometheusRule creates a PrometheusRule for hardware health alerts
// Note: Group-level labels require Prometheus >= 3.0.0 (OCP 4.19+)
func NewIronicPrometheusRule(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PrometheusRule",
			"metadata": map[string]interface{}{
				"name":      ironicPrometheusRuleName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					cboLabelName: stateService,
				},
			},
			"spec": map[string]interface{}{
				"groups": []interface{}{
					map[string]interface{}{
						"name": "baremetal.health",
						"labels": map[string]interface{}{
							"type":      "hardware",
							"component": "ironic",
						},
						"rules": []interface{}{
							// Temperature alerts
							map[string]interface{}{
								"alert": "BaremetalTemperatureHealth",
								"expr":  "last_over_time(baremetal_temperature_status[5m]) == 1",
								"for":   "2m",
								"labels": map[string]interface{}{
									"severity": "warning",
								},
								"annotations": map[string]interface{}{
									"summary":     "Temperature warning on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Warning status on {{ $labels.node_name }}",
								},
							},
							map[string]interface{}{
								"alert": "BaremetalTemperatureHealth",
								"expr":  "last_over_time(baremetal_temperature_status[5m]) == 2",
								"for":   "0m",
								"labels": map[string]interface{}{
									"severity": "critical",
								},
								"annotations": map[string]interface{}{
									"summary":     "Temperature critical on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Critical status on {{ $labels.node_name }}",
								},
							},
							// Power alerts
							map[string]interface{}{
								"alert": "BaremetalPowerHealth",
								"expr":  "last_over_time(baremetal_power_status[5m]) == 1",
								"for":   "2m",
								"labels": map[string]interface{}{
									"severity": "warning",
								},
								"annotations": map[string]interface{}{
									"summary":     "Power warning on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Warning status on {{ $labels.node_name }}",
								},
							},
							map[string]interface{}{
								"alert": "BaremetalPowerHealth",
								"expr":  "last_over_time(baremetal_power_status[5m]) == 2",
								"for":   "0m",
								"labels": map[string]interface{}{
									"severity": "critical",
								},
								"annotations": map[string]interface{}{
									"summary":     "Power critical on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Critical status on {{ $labels.node_name }}",
								},
							},
							// Fan alerts
							map[string]interface{}{
								"alert": "BaremetalFanHealth",
								"expr":  "last_over_time(baremetal_fan_status[5m]) == 1",
								"for":   "2m",
								"labels": map[string]interface{}{
									"severity": "warning",
								},
								"annotations": map[string]interface{}{
									"summary":     "Fan warning on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Warning status on {{ $labels.node_name }}",
								},
							},
							map[string]interface{}{
								"alert": "BaremetalFanHealth",
								"expr":  "last_over_time(baremetal_fan_status[5m]) == 2",
								"for":   "0m",
								"labels": map[string]interface{}{
									"severity": "critical",
								},
								"annotations": map[string]interface{}{
									"summary":     "Fan critical on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Critical status on {{ $labels.node_name }}",
								},
							},
							// Drive alerts
							map[string]interface{}{
								"alert": "BaremetalDriveHealth",
								"expr":  "last_over_time(baremetal_drive_status[5m]) == 1",
								"for":   "2m",
								"labels": map[string]interface{}{
									"severity": "warning",
								},
								"annotations": map[string]interface{}{
									"summary":     "Drive warning on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Warning status on {{ $labels.node_name }}",
								},
							},
							map[string]interface{}{
								"alert": "BaremetalDriveHealth",
								"expr":  "last_over_time(baremetal_drive_status[5m]) == 2",
								"for":   "0m",
								"labels": map[string]interface{}{
									"severity": "critical",
								},
								"annotations": map[string]interface{}{
									"summary":     "Drive critical on {{ $labels.node_name }}",
									"description": "Sensor {{ $labels.sensor_id }} reports Critical status on {{ $labels.node_name }}",
								},
							},
						},
					},
					map[string]interface{}{
						"name": "baremetal.monitoring",
						"labels": map[string]interface{}{
							"type":      "hardware",
							"component": "ironic",
						},
						"rules": []interface{}{
							// Monitoring health
							map[string]interface{}{
								"alert": "BaremetalStaleMetrics",
								"expr":  "(time() - baremetal_last_payload_timestamp_seconds) > 300",
								"for":   "5m",
								"labels": map[string]interface{}{
									"severity": "warning",
								},
								"annotations": map[string]interface{}{
									"summary":     "Stale hardware metrics for {{ $labels.node_name }}",
									"description": "No hardware data received from {{ $labels.node_name }} for {{ $value | humanizeDuration }}",
								},
							},
						},
					},
				},
			},
		},
	}
}

// EnsureIronicPrometheusRule ensures the PrometheusRule exists when sensor metrics and default rules are enabled
func EnsureIronicPrometheusRule(info *ProvisioningInfo) (bool, error) {
	ctx := context.Background()

	// If metrics are disabled or default rules are disabled, ensure PrometheusRule is deleted
	if info.ProvConfig.Spec.PrometheusExporter == nil ||
		!info.ProvConfig.Spec.PrometheusExporter.Enabled ||
		info.ProvConfig.Spec.PrometheusExporter.DisableDefaultPrometheusRules {
		return false, DeleteIronicPrometheusRule(info)
	}

	prometheusRule := NewIronicPrometheusRule(info.Namespace)
	if err := controllerutil.SetControllerReference(info.ProvConfig, prometheusRule, info.Scheme); err != nil {
		return false, fmt.Errorf("unable to set controllerReference on PrometheusRule: %w", err)
	}

	// Apply or Update
	_, updated, err := resourceapply.ApplyPrometheusRule(ctx, info.DynamicClient, info.EventRecorder, prometheusRule)
	if err != nil {
		return false, fmt.Errorf("failed to apply PrometheusRule: %w", err)
	}

	return updated, nil
}

// DeleteIronicPrometheusRule deletes the PrometheusRule
func DeleteIronicPrometheusRule(info *ProvisioningInfo) error {
	prometheusRule := NewIronicPrometheusRule(info.Namespace)
	_, _, err := resourceapply.DeletePrometheusRule(context.Background(), info.DynamicClient, info.EventRecorder, prometheusRule)
	return err
}
