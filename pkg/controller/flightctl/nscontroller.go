package flightctl

import (
	"context"
	"os"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const NamespaceControllerName = "flightctl-namespace-controller"

var log = logf.Log.WithName(NamespaceControllerName)

var _ reconcile.Reconciler = &NSReconciler{}

// NSReconciler is responsible for creating the FlightCtl rbac resources when namespace `flightctl` is created.
// The service account "flightctl-client" is binded `managedcluster-import-controller-agent-registration-client`.
// The token will be used in the `Repository` and delivered to the devices, to let the devices access the
// agent-registration and get klusterlet manifests used for registration.
type NSReconciler struct {
	clientHolder *helpers.ClientHolder
	flightctl    FlightCtler
	recorder     events.Recorder
	scheme       *runtime.Scheme
}

func (r *NSReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconciling FlightCtl namespace", "namespace", request.Name)
	var err error

	// Do nothing if flightctl is not enabled.
	if ok, err := r.flightctl.IsFlightCtlEnabled(ctx); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		return reconcile.Result{}, nil
	}

	// Get the FlightCtl namespace
	// If ns found, create resources and set owner reference to the ns.
	ns := &corev1.Namespace{}
	err = r.clientHolder.RuntimeClient.Get(ctx, request.NamespacedName, ns)
	if errors.IsNotFound(err) {
		// if ns not found, delete the Repository we created
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, err
	}

	if ns.Name != FlightCtlNamespace {
		return reconcile.Result{}, nil
	}

	// Create rbac resources and set owner reference to the ns.
	objects, err := helpers.FilesToObjects(files, struct {
		Namespace    string
		PodNamespace string
	}{
		Namespace:    FlightCtlNamespace,
		PodNamespace: os.Getenv("POD_NAMESPACE"),
	}, &FlightCtlManifestFiles)
	if err != nil {
		return reconcile.Result{}, err
	}
	if _, err := helpers.ApplyResources(
		r.clientHolder, r.recorder, r.scheme, nil, objects...); err != nil {
		return reconcile.Result{}, err
	}

	// Create Repository resources
	err = r.flightctl.ApplyRepository(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func AddNSController(ctx context.Context, mgr manager.Manager, flightctler FlightCtler, clientHolder *helpers.ClientHolder) error {
	err := ctrl.NewControllerManagedBy(mgr).Named(NamespaceControllerName).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{Name: o.GetName()},
					},
				}
			}),
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return e.Object.GetName() == FlightCtlNamespace },
				UpdateFunc:  func(e event.UpdateEvent) bool { return e.ObjectNew.GetName() == FlightCtlNamespace },
			}),
		).
		Complete(&NSReconciler{
			clientHolder: clientHolder,
			flightctl:    flightctler,
			scheme:       mgr.GetScheme(),
			recorder:     helpers.NewEventRecorder(clientHolder.KubeClient, NamespaceControllerName),
		})
	if err != nil {
		return err
	}

	return nil
}
