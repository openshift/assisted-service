/*
Copyright 2021.

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
	"os"

	certtypes "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	NamespaceEnvVar string = "NAMESPACE"
	PodNameEnvVar   string = "POD_NAME"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(aiv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	utilruntime.Must(routev1.AddToScheme(scheme))

	utilruntime.Must(monitoringv1.AddToScheme(scheme))

	utilruntime.Must(apiregv1.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	utilruntime.Must(clusterv1.AddToScheme(scheme))

	utilruntime.Must(configv1.AddToScheme(scheme))

	utilruntime.Must(hivev1.AddToScheme(scheme))

	utilruntime.Must(hiveext.AddToScheme(scheme))

	utilruntime.Must(certtypes.AddToScheme(scheme))
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
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "86f835c3.agent-install.openshift.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ns, found := os.LookupEnv(NamespaceEnvVar)
	if !found {
		setupLog.Error(fmt.Errorf("%s environment variable must be set (commonly set automatically in every Pod)", NamespaceEnvVar), "unable to get namespace")
		os.Exit(1)
	}

	// must have OS images specified on the operator
	// this prevents us from having to include the full json in source
	// ie. this should ALWAYS be specified on the CSV, until a proper
	// API is provided for it
	// I think it's reasonable to check that the OS_IMAGES is
	// legit before we go passing it down to the assisted-service deployment
	// and letting it fail there.
	var osImagesArray models.OsImages
	osImages, found := os.LookupEnv(controllers.OsImagesEnvVar)
	if !found || osImages == "" {
		setupLog.Error(fmt.Errorf("%s environment variable must be set (commonly set automatically in every Pod) to a non-empty value", controllers.OsImagesEnvVar), "unable to get OS images")
		os.Exit(1)
	}
	if err = json.Unmarshal([]byte(osImages), &osImagesArray); err != nil {
		setupLog.Error(fmt.Errorf("OS images (%v) specified in %s are not valid", osImages, controllers.OsImagesEnvVar), "invalid OS images")
		os.Exit(1)
	}

	var nodeSelector map[string]string
	var tolerations []corev1.Toleration

	podName, found := os.LookupEnv(PodNameEnvVar)
	if found {
		client, clientErr := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
		if err != nil {
			setupLog.Error(clientErr, "Unable to create new client")
			os.Exit(1)
		}

		operatorPod := &corev1.Pod{}
		if err = client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: ns}, operatorPod); err != nil {
			setupLog.Error(err, "Unable to get Infrastructure Operator Pod")
			os.Exit(1)
		}
		nodeSelector = operatorPod.Spec.NodeSelector
		tolerations = operatorPod.Spec.Tolerations
	}

	log := logrus.New()
	spokeClientFactory := spoke_k8s_client.NewSpokeK8sClientFactory(log)
	spokeClientCache := controllers.NewSpokeClientCache(spokeClientFactory)

	c, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		log.WithError(err).Error("failed to initialize client")
		os.Exit(1)
	}
	isOpenShift, err := controllers.ServerIsOpenShift(context.Background(), c)
	if err != nil {
		log.WithError(err).Error("failed to check server ")
		os.Exit(1)
	}

	if err = (&controllers.AgentServiceConfigReconciler{
		AgentServiceConfigReconcileContext: controllers.AgentServiceConfigReconcileContext{
			Log:          log,
			Scheme:       mgr.GetScheme(),
			NodeSelector: nodeSelector,
			Tolerations:  tolerations,
			Recorder:     mgr.GetEventRecorderFor("agentserviceconfig-controller"),
			IsOpenShift:  isOpenShift,
		},
		Client:    mgr.GetClient(),
		Namespace: ns,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentServiceConfig")
		os.Exit(1)
	}

	if err = (&controllers.HypershiftAgentServiceConfigReconciler{
		AgentServiceConfigReconcileContext: controllers.AgentServiceConfigReconcileContext{
			Log:          log,
			Scheme:       mgr.GetScheme(),
			NodeSelector: nodeSelector,
			Tolerations:  tolerations,
			Recorder:     mgr.GetEventRecorderFor("hypershiftagentserviceconfig-controller"),
			IsOpenShift:  isOpenShift,
		},
		Client:       mgr.GetClient(),
		SpokeClients: spokeClientCache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HypershiftAgentServiceConfig")
		os.Exit(1)
	}

	// local cluster import is only relevant if assisted installer can manage/scale the local cluster and this is only the case for OpenShift
	if isOpenShift {
		if err = (controllers.NewLocalClusterImportReconciler(mgr.GetClient(), "local-cluster", controllers.AgentServiceConfigName, log)).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LocalClusterImportReconciler")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
