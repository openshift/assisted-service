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
	"encoding/json"
	"flag"
	"fmt"
	"os"

	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	NamespaceEnvVar string = "NAMESPACE"
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
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
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

	// must have openshift versions specified on the operator
	// this prevents us from having to include the full json in source
	// ie. this should ALWAYS be specified on the CSV, until a proper
	// API is provided for it
	// I think it's reasonable to check that the OPENSHIFT_VERSIONS is
	// legit before we go passing it down to the assisted-service deployment
	// and letting it fail there.
	var openshiftVersionsMap models.OpenshiftVersions
	openshiftVersions, found := os.LookupEnv(controllers.OpenshiftVersionsEnvVar)
	if !found || openshiftVersions == "" {
		setupLog.Error(fmt.Errorf("%s environment variable must be set (commonly set automatically in every Pod) to a non-empty value.", controllers.OpenshiftVersionsEnvVar), "unable to get OpenShift Versions")
		os.Exit(1)
	}
	if err = json.Unmarshal([]byte(openshiftVersions), &openshiftVersionsMap); err != nil {
		setupLog.Error(fmt.Errorf("OpenShift versions (%v) specified in %s are not valid", openshiftVersions, controllers.OpenshiftVersionsEnvVar), "invalid OpenShift Versions")
		os.Exit(1)
	}

	if err = (&controllers.AgentServiceConfigReconciler{
		Client:    mgr.GetClient(),
		Log:       logrus.New(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("agentserviceconfig-controller"),
		Namespace: ns,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentServiceConfig")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

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
