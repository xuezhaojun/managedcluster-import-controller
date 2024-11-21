package agentregistration

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	CSRControllerName              = "agent-registration-csr-controller"
	AgentRegistrationBootstrapUser = "system:serviceaccount:multicluster-engine:agent-registration-bootstrap"
)

var logger = log.Log.WithName("controller_agentregistration_csr")

// CSRReconciler reconciles the agent-registration CSRs
type CSRReconciler struct {
	clientHolder       *helpers.ClientHolder
	recorder           events.Recorder
	approvalConditions []func(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) (bool, error)
}

var _ reconcile.Reconciler = &CSRReconciler{}

func (r *CSRReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logger.WithValues("Request.Name", request.Name)

	csrReq := r.clientHolder.KubeClient.CertificatesV1().CertificateSigningRequests()
	csr, err := csrReq.Get(ctx, request.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, err
	}
	if helpers.IsCSRInTerminalState(&csr.Status) {
		return reconcile.Result{}, nil
	}

	// Check if any approval condition matches
	shouldApprove := false
	for _, condition := range r.approvalConditions {
		matched, err := condition(ctx, csr)
		if err != nil {
			return reconcile.Result{}, err
		}
		if matched {
			shouldApprove = true
			break
		}
	}

	if !shouldApprove {
		return reconcile.Result{}, nil
	}

	reqLogger.Info("Reconciling CSR")

	csr = csr.DeepCopy()
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Status:         corev1.ConditionTrue,
		Reason:         "AutoApprovedByCSRController",
		Message:        "The agent-registration csr controller automatically approved this CSR",
		LastUpdateTime: metav1.Now(),
	})
	if _, err := csrReq.UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{}); err != nil {
		return reconcile.Result{}, err
	}

	r.recorder.Eventf("ManagedClusterCSRAutoApproved", "agent-registration csr %q is auto approved by import controller", csr.Name)
	return reconcile.Result{}, nil
}

func AddCSRController(ctx context.Context,
	mgr manager.Manager, clientHolder *helpers.ClientHolder,
	approvalConditions []func(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) (bool, error)) error {
	err := ctrl.NewControllerManagedBy(mgr).Named(CSRControllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: helpers.GetMaxConcurrentReconciles(),
		}).
		Watches(
			&certificatesv1.CertificateSigningRequest{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				UpdateFunc: func(e event.UpdateEvent) bool {
					return isValidUnapprovedBootstrapCSR(e.ObjectNew.(*certificatesv1.CertificateSigningRequest))
				},
				CreateFunc: func(e event.CreateEvent) bool {
					return isValidUnapprovedBootstrapCSR(e.Object.(*certificatesv1.CertificateSigningRequest))
				},
			}),
		).
		Complete(&CSRReconciler{
			clientHolder:       clientHolder,
			recorder:           helpers.NewEventRecorder(clientHolder.KubeClient, CSRControllerName),
			approvalConditions: approvalConditions,
		})
	return err
}

// isValidUnapprovedBootstrapCSR checks if the CSR:
// 1. Has a non-empty cluster name label
// 2. Has not been approved or denied
// 3. Is from the agent-registration bootstrap user
func isValidUnapprovedBootstrapCSR(csr *certificatesv1.CertificateSigningRequest) bool {
	clusterName := helpers.GetClusterName(csr)
	return clusterName != "" && helpers.GetApprovalType(csr) == "" && csr.Spec.Username == AgentRegistrationBootstrapUser
}
