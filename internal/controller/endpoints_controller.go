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

package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	// Name is the name of the operator
	Name = "endpoint-copier-operator"

	// Annotation used on services to enable endpoints syncing
	ServiceAnnotationEnabled          = "endpoint-copier/enabled"
	AnnotationDefaultServiceName      = "endpoint-copier/default-service-name"
	AnnotationDefaultServiceNamespace = "endpoint-copier/default-service-namespace"
)

// EndpointsReconciler reconciles a Endpoints object
type EndpointsReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	DefaultEndpointName      string
	DefaultEndpointNamespace string
	ManagedEndpointName      string
	ManagedEndpointNamespace string
	ApiserverPort            int
	ApiserverProtocol        string
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Endpoints object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *EndpointsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.Log.WithName("endpoints")

	// Fetch the Service that triggered the reconcile
	svc := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Service not found", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	annotations := svc.GetAnnotations()
	enabled := annotations != nil && annotations[ServiceAnnotationEnabled] == "true"

	var managedServiceName, managedServiceNamespace string
	var defaultServiceName, defaultServiceNamespace string

	if enabled {
		// If annotation enabled: The managed service is the current Service itself
		managedServiceName = svc.Name
		managedServiceNamespace = svc.Namespace

		// The default service is read from the annotations on the managed service
		defaultServiceName = annotations[AnnotationDefaultServiceName]
		defaultServiceNamespace = annotations[AnnotationDefaultServiceNamespace]

		logger.Info("Annotation enabled: using dynamic managed and default services",
			"managedServiceName", managedServiceName, "managedServiceNamespace", managedServiceNamespace,
			"defaultServiceName", defaultServiceName, "defaultServiceNamespace", defaultServiceNamespace)
	} else {
		// Legacy mode fallback - use configured fixed names and namespaces
		managedServiceName = r.ManagedEndpointName
		managedServiceNamespace = r.ManagedEndpointNamespace

		defaultServiceName = r.DefaultEndpointName
		defaultServiceNamespace = r.DefaultEndpointNamespace

		logger.Info("Annotation not enabled, using legacy static configuration — this behavior is DEPRECATED",
			"managedServiceName", managedServiceName, "managedServiceNamespace", managedServiceNamespace,
			"defaultServiceName", defaultServiceName, "defaultServiceNamespace", defaultServiceNamespace)
	}

	// Get the managed Service object
	managedService := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: managedServiceNamespace, Name: managedServiceName}, managedService); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Managed Service not found", "name", managedServiceName, "namespace", managedServiceNamespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Error getting managed service")
		return ctrl.Result{}, err
	}

	// Get the default Endpoints object
	endpoints := &corev1.Endpoints{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: defaultServiceNamespace, Name: defaultServiceName}, endpoints); err != nil {
		return ctrl.Result{}, err
	}

	// Sync endpoints from default to managed service
	if err := r.syncEndpoints(ctx, logger, endpoints, managedService); err != nil {
		logger.Error(err, "error syncing endpoints")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated endpoint", "name", managedServiceName, "namespace", managedServiceNamespace)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EndpointsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return e.Object.GetNamespace() == r.DefaultEndpointNamespace && e.Object.GetName() == r.DefaultEndpointName
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectOld.GetNamespace() == r.DefaultEndpointNamespace && e.ObjectOld.GetName() == r.DefaultEndpointName
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return e.Object.GetNamespace() == r.DefaultEndpointNamespace && e.Object.GetName() == r.DefaultEndpointName
			},
		})).
		Watches(&corev1.Service{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return (e.Object.GetNamespace() == r.ManagedEndpointNamespace && e.Object.GetName() == r.ManagedEndpointName) || hasEndpointCopierEnabledAnnotation(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectNew.GetNamespace() == r.ManagedEndpointNamespace && e.ObjectNew.GetName() == r.ManagedEndpointName || hasEndpointCopierEnabledAnnotation(e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return e.Object.GetNamespace() == r.ManagedEndpointNamespace && e.Object.GetName() == r.ManagedEndpointName || hasEndpointCopierEnabledAnnotation(e.Object)
			},
		})).Complete(r)
}

// syncEndpoint updates the Endpoint resource with the current node IPs.
func (r *EndpointsReconciler) syncEndpoints(ctx context.Context, logger logr.Logger, defaultEndpoints *corev1.Endpoints, managedService *corev1.Service) error {
	managedEndpoints := &corev1.Endpoints{}
	managedEndpoints.ObjectMeta.Name = managedService.Name
	managedEndpoints.ObjectMeta.Namespace = managedService.Namespace
	managedEndpoints.ObjectMeta.Labels = map[string]string{"endpointslice.kubernetes.io/managed-by": Name}

	managedEndpoints.Subsets = []corev1.EndpointSubset{}
	for _, subset := range defaultEndpoints.Subsets {
		var copiedPorts []corev1.EndpointPort
		for _, port := range managedService.Spec.Ports {
			var portNumber int32
			if port.TargetPort.Type == intstr.Int {
				portNumber = port.TargetPort.IntVal
			} else {
				portNumber = port.Port
			}
			endpointPort := corev1.EndpointPort{
				Name:     port.Name,
				Port:     portNumber,
				Protocol: port.Protocol,
			}
			copiedPorts = append(copiedPorts, endpointPort)
		}

		// Copy the addresses
		copiedAddresses := make([]corev1.EndpointAddress, len(subset.Addresses))
		copy(copiedAddresses, subset.Addresses)

		newSubset := corev1.EndpointSubset{
			Addresses: copiedAddresses,
			Ports:     copiedPorts,
		}

		managedEndpoints.Subsets = append(managedEndpoints.Subsets, newSubset)
	}

	// Update the custom Endpoints resource with the updated IP addresses.
	if err := r.Update(ctx, managedEndpoints); err != nil {
		return err
	}

	return nil
}

// helper func to check annotation
func hasEndpointCopierEnabledAnnotation(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, ok := annotations[ServiceAnnotationEnabled]
	return ok && val == "true"
}
