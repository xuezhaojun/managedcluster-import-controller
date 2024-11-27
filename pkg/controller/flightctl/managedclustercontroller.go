package flightctl

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const ManagedClusterControllerName = "flightctl-managedcluster-controller"

// ManagedClusterReconciler is responsible to set hubAcceptsClient to true if the managed cluster is a flightctl device.
type ManagedClusterReconciler struct {
	clientHolder *helpers.ClientHolder
	recorder     events.Recorder
	flightctl    FlightCtler
}

var _ reconcile.Reconciler = &ManagedClusterReconciler{}

func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Do nothing if flightctl is not enabled.
	if ok, err := r.flightctl.IsFlightCtlEnabled(ctx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{}, nil
	}

	cluster := &clusterv1.ManagedCluster{}
	if err := r.clientHolder.RuntimeClient.Get(ctx, request.NamespacedName, cluster); err != nil {
		return reconcile.Result{}, err
	}

	if cluster.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	if cluster.Spec.HubAcceptsClient {
		return reconcile.Result{}, nil
	}

	isDevice, err := r.flightctl.IsManagedClusterAFlightctlDevice(ctx, cluster.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !isDevice {
		return reconcile.Result{}, nil
	}

	cluster.Spec.HubAcceptsClient = true
	if err := r.clientHolder.RuntimeClient.Update(ctx, cluster); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func AddManagedClusterController(ctx context.Context, mgr manager.Manager, flightctler FlightCtler, clientHolder *helpers.ClientHolder) error {
	err := ctrl.NewControllerManagedBy(mgr).Named(ManagedClusterControllerName).
		Watches(
			&clusterv1.ManagedCluster{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				UpdateFunc: func(e event.UpdateEvent) bool {
					return !e.ObjectNew.(*clusterv1.ManagedCluster).Spec.HubAcceptsClient
				},
				CreateFunc: func(e event.CreateEvent) bool {
					return !e.Object.(*clusterv1.ManagedCluster).Spec.HubAcceptsClient
				},
			})).
		Complete(&ManagedClusterReconciler{
			clientHolder: clientHolder,
			flightctl:    flightctler,
			recorder:     helpers.NewEventRecorder(clientHolder.KubeClient, ManagedClusterControllerName),
		})

	return err
}
