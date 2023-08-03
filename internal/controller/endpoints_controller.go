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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	// Name is the name of the operator
	Name = "endpoint-copier-operator"
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

	// Fetch the Endpoints object
	endpoints := &corev1.Endpoints{}
	if err := r.Get(ctx, req.NamespacedName, endpoints); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// update the endpoints
	if err := r.syncEndpoints(ctx, logger, endpoints); err != nil {
		logger.Error(err, "error syncing endpoint")
		return reconcile.Result{}, err
	}

	logger.Info("Successfully updated endpoint", "name", r.ManagedEndpointName, "namespace", r.ManagedEndpointNamespace)

	return reconcile.Result{}, nil
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
		Complete(r)
}

// syncEndpoint updates the Endpoint resource with the current node IPs.
func (r *EndpointsReconciler) syncEndpoints(ctx context.Context, logger logr.Logger, defaultEndpoints *corev1.Endpoints) error {
	managedEndpoints := &corev1.Endpoints{}
	managedEndpoints.ObjectMeta.Name = r.ManagedEndpointName
	managedEndpoints.ObjectMeta.Namespace = r.ManagedEndpointNamespace
	managedEndpoints.ObjectMeta.Labels = map[string]string{"endpointslice.kubernetes.io/managed-by": Name}

	// Copy only subset addresses without the ports
	managedEndpoints.Subsets = []corev1.EndpointSubset{}
	for _, subset := range defaultEndpoints.Subsets {
		subset.Ports = []corev1.EndpointPort{{Port: int32(r.ApiserverPort), Protocol: corev1.Protocol(r.ApiserverProtocol)}}
		managedEndpoints.Subsets = append(managedEndpoints.Subsets, subset)
	}

	// Update the custom Endpoints resource with the updated IP addresses.
	if err := r.Update(ctx, managedEndpoints); err != nil {
		return err
	}

	return nil
}
