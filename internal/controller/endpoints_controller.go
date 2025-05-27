package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	Name                              = "endpoint-copier-operator"
	ServiceAnnotationEnabled          = "endpoint-copier/enabled"
	AnnotationDefaultServiceName      = "endpoint-copier/default-service-name"
	AnnotationDefaultServiceNamespace = "endpoint-copier/default-service-namespace"
)

type EndpointSliceReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	DefaultEndpointName      string
	DefaultEndpointNamespace string
	ManagedEndpointName      string
	ManagedEndpointNamespace string
	ApiserverPort            int
	ApiserverProtocol        string
}

func (r *EndpointSliceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.Log.WithName("endpointslice")

	svc := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Service not found, cleaning up EndpointSlices", "name", req.Name, "namespace", req.Namespace)

			var slices discoveryv1.EndpointSliceList
			err := r.List(ctx, &slices, client.InNamespace(req.Namespace), client.MatchingLabels{
				"kubernetes.io/service-name": req.Name,
			})
			if err != nil {
				logger.Error(err, "Failed to list EndpointSlices for cleanup")
				return ctrl.Result{}, err
			}

			for _, slice := range slices.Items {
				// Optional: only delete slices managed by your controller
				if slice.Labels["endpoint-copier/source"] != "" {
					if err := r.Delete(ctx, &slice); err != nil && !apierrors.IsNotFound(err) {
						logger.Error(err, "Failed to delete EndpointSlice", "name", slice.Name)
						// Don't return yet; continue trying to clean up others
					} else {
						logger.Info("Deleted EndpointSlice", "name", slice.Name)
					}
				}
			}

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
		managedServiceName = svc.Name
		managedServiceNamespace = svc.Namespace
		defaultServiceName = annotations[AnnotationDefaultServiceName]
		defaultServiceNamespace = annotations[AnnotationDefaultServiceNamespace]
	} else {
		managedServiceName = r.ManagedEndpointName
		managedServiceNamespace = r.ManagedEndpointNamespace
		defaultServiceName = r.DefaultEndpointName
		defaultServiceNamespace = r.DefaultEndpointNamespace
	}

	managedService := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: managedServiceNamespace, Name: managedServiceName}, managedService); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Managed Service not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var slices discoveryv1.EndpointSliceList
	err := r.List(ctx, &slices, client.MatchingLabels{"kubernetes.io/service-name": defaultServiceName}, client.InNamespace(defaultServiceNamespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.syncEndpointSlices(ctx, logger, slices.Items, managedService); err != nil {
		logger.Error(err, "Error syncing endpoint slices")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated endpoint slices", "name", managedServiceName, "namespace", managedServiceNamespace)
	return ctrl.Result{}, nil
}

func (r *EndpointSliceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1.EndpointSlice{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return e.Object.GetLabels()["kubernetes.io/service-name"] == r.DefaultEndpointName && e.Object.GetNamespace() == r.DefaultEndpointNamespace
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectOld.GetLabels()["kubernetes.io/service-name"] == r.DefaultEndpointName && e.ObjectOld.GetNamespace() == r.DefaultEndpointNamespace
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return e.Object.GetLabels()["kubernetes.io/service-name"] == r.DefaultEndpointName && e.Object.GetNamespace() == r.DefaultEndpointNamespace
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

func (r *EndpointSliceReconciler) syncEndpointSlices(ctx context.Context, logger logr.Logger, sourceSlices []discoveryv1.EndpointSlice, managedService *corev1.Service) error {
	for _, src := range sourceSlices {
		copiedPorts := []discoveryv1.EndpointPort{}
		for _, port := range managedService.Spec.Ports {
			portNum := port.Port
			if port.TargetPort.Type == intstr.Int {
				portNum = port.TargetPort.IntVal
			}
			copiedPorts = append(copiedPorts, discoveryv1.EndpointPort{
				Name:     &port.Name,
				Port:     &portNum,
				Protocol: &port.Protocol,
			})
		}

		newSlice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedService.Name,
				Namespace: managedService.Namespace,
				Labels: map[string]string{
					"kubernetes.io/service-name": managedService.Name,
					"endpoint-copier/source":     src.Name,
				},
			},
			AddressType: src.AddressType,
			Endpoints:   src.Endpoints,
			Ports:       copiedPorts,
		}

		newSlice.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "discovery.k8s.io",
			Version: "v1",
			Kind:    "EndpointSlice",
		})

		// Upsert logic
		err := r.Patch(ctx, newSlice, client.Apply, client.ForceOwnership, client.FieldOwner(Name))
		if err != nil {
			logger.Error(err, "Failed to patch EndpointSlice", "name", newSlice.Name)
		}
	}
	return nil
}

func hasEndpointCopierEnabledAnnotation(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, ok := annotations[ServiceAnnotationEnabled]
	return ok && val == "true"
}
