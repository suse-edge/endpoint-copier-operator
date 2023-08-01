/*
Copyright 2023.

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
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/component-base/version"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github/endpoint-copier-operator/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr              string
		enableLeaderElection     bool
		probeAddr                string
		defaultEndpointName      string
		defaultEndpointNamespace string
		managedEndpointName      string
		managedEndpointNamespace string
		apiserverHttpsPort       int
		apiserverHttpsProtocol   string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&defaultEndpointName, "default-endpoint-name", "kubernetes", "The name of the default endpoint in the cluster.")
	flag.StringVar(&defaultEndpointNamespace, "default-endpoint-namespace", "default", "The namespace of the default endpoint in the cluster.")
	flag.StringVar(&managedEndpointName, "managed-endpoint-name", "kubernetes-vip", "The name of the managed endpoint in the cluster.")
	flag.StringVar(&managedEndpointNamespace, "managed-endpoint-namespace", "default", "The namespace of the managed endpoint in the cluster.")
	flag.IntVar(&apiserverHttpsPort, "apiserver-port", 6443, "The https port of the kube-apiserver.")
	flag.StringVar(&apiserverHttpsProtocol, "apiserver-protocol", "TCP", "The https protocol of the kube-apiserver.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Print the values of the flags
	setupLog.Info("", "Metrics Address", metricsAddr)
	setupLog.Info("", "Health Probe Address", probeAddr)
	setupLog.Info("", "Leader Election Enabled", enableLeaderElection)
	setupLog.Info("", "Default Endpoint Name", defaultEndpointName)
	setupLog.Info("", "Default Endpoint Namespace", defaultEndpointNamespace)
	setupLog.Info("", "Managed Endpoint Name", managedEndpointName)
	setupLog.Info("", "Managed Endpoint Namespace", managedEndpointNamespace)
	setupLog.Info("", "APIServer HTTPS Port", apiserverHttpsPort)
	setupLog.Info("", "APIServer HTTPS Protocol", apiserverHttpsProtocol)

	setupLog.Info("Starting Endpoints Operator", "Version", version.Get())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f668ae63.endpoint-copier-operator",
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

	if err = (&controller.EndpointsReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		DefaultEndpointName:      defaultEndpointName,
		DefaultEndpointNamespace: defaultEndpointNamespace,
		ManagedEndpointName:      managedEndpointName,
		ManagedEndpointNamespace: managedEndpointNamespace,
		ApiserverPort:            apiserverHttpsPort,
		ApiserverProtocol:        apiserverHttpsProtocol,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Endpoints")
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

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
