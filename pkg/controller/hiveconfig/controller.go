// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package hiveconfig

import (
	"context"
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	openshiftclientset "github.com/openshift/client-go/config/clientset/versioned"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"

	configv1 "github.com/openshift/api/config/v1"
	kevents "k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// ControllerName is the name of this controller
	ControllerName = "hiveconfig"

	// Namespace where the target secret will be created in managed clusters
	openshiftConfigNamespace = "openshift-config"

	// The namespace where the HiveConfig is located
	HiveNamespace = "hive"

	// AnnotationHiveAPITLSCertSecretName is the annotation key that contains the name of the secret that contains the Hive API TLS certificate bundle
	AnnotationHiveAPITLSCertSecretName = "managedcluster-import-controller.open-cluster-management.io/hive-api-tls-cert-secret"

	// AdditionalCASecretName is the name of the secret that will be created in the managed cluster
	AdditionalCASecretName = "acm-additional-ca"

	// AdditionalServingCertName is the name of the secret that will be created in the managed cluster
	AdditionalServingCertSecretName = "acm-serving-cert"
)

// Define logger consistent with clusterdeployment_controller.go
var log = logf.Log.WithName(ControllerName)

// newReconciler returns a new reconcile.Reconciler
func newReconciler(clientHolder *helpers.ClientHolder, mcRecorder kevents.EventRecorder) reconcile.Reconciler {
	return &ReconcileHiveConfig{
		hubClientHolder: clientHolder,
		mcRecorder:      mcRecorder,
	}
}

// Add adds a new Controller to mgr with r as the reconcile.Reconciler
func Add(mgr manager.Manager, clientHolder *helpers.ClientHolder, mcRecorder kevents.EventRecorder) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		Watches(
			&hivev1.HiveConfig{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldHiveConfig, ok := e.ObjectOld.(*hivev1.HiveConfig)
					if !ok {
						return false
					}
					newHiveConfig, ok := e.ObjectNew.(*hivev1.HiveConfig)
					if !ok {
						return false
					}

					// Get the old and new annotation values
					oldAnnotation := ""
					if oldHiveConfig.Annotations != nil {
						oldAnnotation = oldHiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName]
					}

					newAnnotation := ""
					if newHiveConfig.Annotations != nil {
						newAnnotation = newHiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName]
					}

					// Only reconcile if the annotation has changed
					return oldAnnotation != newAnnotation
				},
				CreateFunc: func(e event.CreateEvent) bool {
					hiveConfig, ok := e.Object.(*hivev1.HiveConfig)
					if !ok {
						return false
					}

					// Only reconcile if the annotation is set
					return hiveConfig.Annotations != nil && hiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName] != ""
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					// Always reconcile on delete
					return true
				},
				GenericFunc: func(e event.GenericEvent) bool {
					// Don't reconcile on generic events
					return false
				},
			}),
		).
		// Only watch secrets that are referenced by the HiveConfig annotation
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Get the secret
				secret, ok := obj.(*corev1.Secret)
				if !ok {
					return nil
				}

				// Only process secrets in the Hive namespace
				if secret.Namespace != HiveNamespace {
					return nil
				}

				// Get the HiveConfig to check if this secret is referenced in the annotation
				hiveConfig := &hivev1.HiveConfig{}
				err := mgr.GetClient().Get(ctx, types.NamespacedName{Name: HiveNamespace}, hiveConfig)
				if err != nil {
					log.Error(err, "failed to get HiveConfig")
					return nil
				}

				// Check if the secret is referenced in the annotation
				if hiveConfig.Annotations != nil && hiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName] == secret.Name {
					// This secret is referenced by the HiveConfig, trigger a reconcile
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name: HiveNamespace,
							},
						},
					}
				}

				// Not a relevant secret, don't trigger a reconcile
				return nil
			}),
		).
		Complete(newReconciler(clientHolder, mcRecorder))
}

// ReconcileHiveConfig reconciles a HiveConfig object
type ReconcileHiveConfig struct {
	hubClientHolder *helpers.ClientHolder
	mcRecorder      kevents.EventRecorder
}

// Reconcile reads the HiveConfig and manages the additional secret across all managed clusters
func (r *ReconcileHiveConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	hiveConfig := &hivev1.HiveConfig{}
	err := r.hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{Name: HiveNamespace}, hiveConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// HiveConfig not found, nothing to do
			return reconcile.Result{}, nil
		}
		log.Error(err, "error getting HiveConfig")
		return reconcile.Result{}, err
	}

	// Check if additionalSecret reference is configured
	var apiTLSCertSecret *corev1.Secret
	var secretFound bool

	// Check if the annotation is configured
	if hiveConfig.Annotations != nil && hiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName] != "" {
		// Get the referenced secret
		secretName := hiveConfig.Annotations[AnnotationHiveAPITLSCertSecretName]
		secretNamespace := HiveNamespace

		// Get the referenced secret
		apiTLSCertSecret = &corev1.Secret{}
		err = r.hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{
			Namespace: secretNamespace,
			Name:      secretName,
		}, apiTLSCertSecret)
		if err != nil {
			log.Error(err, "error getting Hive API TLS certificate secret")
			return reconcile.Result{}, err
		}

		// Validate the secret has the required keys
		if err := validateTLSSecret(apiTLSCertSecret); err != nil {
			log.Error(err, "invalid TLS certificate secret")
			return reconcile.Result{}, err
		}

		secretFound = true
	}

	// Get all managed clusters
	clusterDeploymentList := &hivev1.ClusterDeploymentList{}
	if err := r.hubClientHolder.RuntimeClient.List(ctx, clusterDeploymentList); err != nil {
		log.Error(err, "error listing managed clusters")
		return reconcile.Result{}, err
	}

	// Process each managed cluster
	for i := range clusterDeploymentList.Items {
		cluster := &clusterDeploymentList.Items[i]
		if !cluster.Spec.Installed {
			// Skip clusters that aren't fully installed yet
			continue
		}

		// Get cluster's API URL
		if cluster.Status.APIURL == "" {
			log.Info("cluster API URL is not set", "cluster", cluster.Name)
			continue
		}
		clusterAPIURL := cluster.Status.APIURL

		// Get cluster's kubeconfig to connect to it
		if cluster.Spec.ClusterMetadata == nil || cluster.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name == "" {
			log.Info("cluster admin kubeconfig is not set", "cluster", cluster.Name)
			continue
		}

		// Get cluster's kubeconfig to connect to it
		clusterClient, err := r.getClusterClient(ctx, cluster)
		if err != nil {
			log.Error(err, "failed to get cluster client", "cluster", cluster.Name)
			continue
		}

		clusterOpenshiftClient, err := r.getClusterOpenshiftClient(ctx, cluster)
		if err != nil {
			log.Error(err, "failed to get cluster OpenShift client", "cluster", cluster.Name)
			continue
		}

		if secretFound {
			// Apply the serving cert secret
			if err := applyServingCertSecret(ctx, clusterClient, apiTLSCertSecret); err != nil {
				log.Error(err, "failed to apply serving cert secret", "cluster", cluster.Name)
				continue
			}

			// Extract hostname from API URL
			hostname, err := extractHostname(clusterAPIURL)
			if err != nil {
				log.Error(err, "failed to extract hostname from API URL", "cluster", cluster.Name, "apiURL", clusterAPIURL)
				continue
			}

			if err := applyAdditionalCAToAPIServer(ctx, clusterOpenshiftClient, hostname); err != nil {
				log.Error(err, "failed to apply additional CA to API server", "cluster", cluster.Name)
				continue
			}
		} else {
			// Remove the serving cert secret
			if err := removeServingCertSecret(ctx, clusterClient); err != nil {
				log.Error(err, "failed to remove serving cert secret", "cluster", cluster.Name)
				continue
			}

			if err := removeAdditionalCAFromAPIServer(ctx, clusterOpenshiftClient); err != nil {
				log.Error(err, "failed to remove additional CA from API server", "cluster", cluster.Name)
				continue
			}
		}
	}

	if secretFound {
		// Apply the additional CA secret to the hub cluster and update HiveConfig
		if err := applyAdditionalCASecretRef(ctx, r.hubClientHolder, apiTLSCertSecret); err != nil {
			log.Error(err, "failed to apply additional CA secret to hub cluster")
			return reconcile.Result{}, err
		}
	} else {
		// Remove the additional CA secret reference from HiveConfig
		if err := removeAdditionalCASecretRef(ctx, r.hubClientHolder, hiveConfig); err != nil {
			log.Error(err, "failed to remove additional CA secret reference from HiveConfig")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// validateTLSSecret validates that the secret has the required keys
func validateTLSSecret(secret *corev1.Secret) error {
	requiredKeys := []string{"tls.key", "tls.crt", "ca.crt"}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			return fmt.Errorf("secret %s/%s is missing required key %s", secret.Namespace, secret.Name, key)
		}
	}
	return nil
}

// extractHostname extracts the hostname from a URL
func extractHostname(apiURL string) (string, error) {
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse API URL %s: %w", apiURL, err)
	}
	return parsedURL.Hostname(), nil
}

// applyAdditionalCASecretRef creates or updates the additional CA secret in the hub cluster
// and updates the HiveConfig to reference it
func applyAdditionalCASecretRef(
	ctx context.Context,
	hubClientHolder *helpers.ClientHolder,
	sourceSecret *corev1.Secret,
) error {
	// Create a new secret with only the CA certificate
	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AdditionalCASecretName,
			Namespace: HiveNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca.crt": sourceSecret.Data["ca.crt"],
		},
	}

	// Try to get existing secret
	existingSecret := &corev1.Secret{}
	err := hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{
		Namespace: HiveNamespace,
		Name:      AdditionalCASecretName,
	}, existingSecret)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create the secret if it doesn't exist
			if err = hubClientHolder.RuntimeClient.Create(ctx, targetSecret); err != nil {
				return fmt.Errorf("failed to create additional CA secret: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get additional CA secret: %w", err)
		}
	} else {
		// Update the secret if it exists
		existingSecret.Data = targetSecret.Data
		if err = hubClientHolder.RuntimeClient.Update(ctx, existingSecret); err != nil {
			return fmt.Errorf("failed to update additional CA secret: %w", err)
		}
	}

	// Update HiveConfig to reference the additional CA secret
	hiveConfig := &hivev1.HiveConfig{}
	if err := hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{Name: HiveNamespace}, hiveConfig); err != nil {
		return fmt.Errorf("failed to get HiveConfig: %w", err)
	}

	// Check if the secret is already referenced
	secretAlreadyReferenced := false
	for _, secretRef := range hiveConfig.Spec.AdditionalCertificateAuthoritiesSecretRef {
		if secretRef.Name == AdditionalCASecretName {
			secretAlreadyReferenced = true
			break
		}
	}

	// Add the secret reference if it's not already there
	if !secretAlreadyReferenced {
		hiveConfig.Spec.AdditionalCertificateAuthoritiesSecretRef = append(
			hiveConfig.Spec.AdditionalCertificateAuthoritiesSecretRef,
			corev1.LocalObjectReference{
				Name: AdditionalCASecretName,
			},
		)

		if err := hubClientHolder.RuntimeClient.Update(ctx, hiveConfig); err != nil {
			return fmt.Errorf("failed to update HiveConfig with additional CA secret reference: %w", err)
		}
	}

	return nil
}

// removeAdditionalCASecretRef removes the additional CA secret reference from HiveConfig
func removeAdditionalCASecretRef(
	ctx context.Context,
	hubClientHolder *helpers.ClientHolder,
	hiveConfig *hivev1.HiveConfig,
) error {
	// Check if the secret is referenced
	var updatedSecretRefs []corev1.LocalObjectReference
	secretRemoved := false

	for _, secretRef := range hiveConfig.Spec.AdditionalCertificateAuthoritiesSecretRef {
		if secretRef.Name != AdditionalCASecretName {
			updatedSecretRefs = append(updatedSecretRefs, secretRef)
		} else {
			secretRemoved = true
		}
	}

	// If the secret wasn't found, nothing to do
	if !secretRemoved {
		return nil
	}

	// Update HiveConfig with the secret reference removed
	hiveConfig.Spec.AdditionalCertificateAuthoritiesSecretRef = updatedSecretRefs
	if err := hubClientHolder.RuntimeClient.Update(ctx, hiveConfig); err != nil {
		return fmt.Errorf("failed to update HiveConfig to remove additional CA secret reference: %w", err)
	}

	// Try to delete the secret
	err := hubClientHolder.RuntimeClient.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AdditionalCASecretName,
			Namespace: HiveNamespace,
		},
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete additional CA secret: %w", err)
	}

	return nil
}

// applyServingCertSecret creates or updates the serving cert secret in the managed cluster
func applyServingCertSecret(
	ctx context.Context,
	clusterClient *kubernetes.Clientset,
	sourceSecret *corev1.Secret,
) error {
	// Create a new secret with the TLS key and certificate
	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AdditionalServingCertSecretName,
			Namespace: openshiftConfigNamespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.key": sourceSecret.Data["tls.key"],
			"tls.crt": sourceSecret.Data["tls.crt"],
		},
	}

	// Try to get existing secret
	_, err := clusterClient.CoreV1().Secrets(openshiftConfigNamespace).Get(ctx, AdditionalServingCertSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the secret if it doesn't exist
			_, err = clusterClient.CoreV1().Secrets(openshiftConfigNamespace).Create(ctx, targetSecret, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update the secret if it exists
	_, err = clusterClient.CoreV1().Secrets(openshiftConfigNamespace).Update(ctx, targetSecret, metav1.UpdateOptions{})
	return err
}

// removeServingCertSecret deletes the serving cert secret from the managed cluster
func removeServingCertSecret(
	ctx context.Context,
	clusterClient *kubernetes.Clientset,
) error {
	err := clusterClient.CoreV1().Secrets(openshiftConfigNamespace).Delete(ctx, AdditionalServingCertSecretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// applyAdditionalCAToAPIServer updates the OpenShift API server configuration to use the additional CA
func applyAdditionalCAToAPIServer(
	ctx context.Context,
	openshiftClient *openshiftclientset.Clientset,
	hostname string,
) error {
	// Get the current APIServer configuration
	apiServer, err := openshiftClient.ConfigV1().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get APIServer configuration: %w", err)
	}

	// Check if the additional CA is already configured
	for _, cert := range apiServer.Spec.ServingCerts.NamedCertificates {
		if cert.ServingCertificate.Name == AdditionalServingCertSecretName {
			// The additional CA is already configured
			return nil
		}
	}

	// Add the additional CA to the APIServer configuration
	apiServer.Spec.ServingCerts.NamedCertificates = append(
		apiServer.Spec.ServingCerts.NamedCertificates,
		configv1.APIServerNamedServingCert{
			// Use the hostname from the API URL
			Names:              []string{hostname},
			ServingCertificate: configv1.SecretNameReference{Name: AdditionalServingCertSecretName},
		},
	)

	// Update the APIServer configuration
	_, err = openshiftClient.ConfigV1().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update APIServer configuration: %w", err)
	}

	return nil
}

// removeAdditionalCAFromAPIServer removes references to the additional CA from the OpenShift API server configuration
func removeAdditionalCAFromAPIServer(
	ctx context.Context,
	openshiftClient *openshiftclientset.Clientset,
) error {
	// Get the current APIServer configuration
	apiServer, err := openshiftClient.ConfigV1().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// The APIServer configuration doesn't exist, nothing to do
			return nil
		}
		return fmt.Errorf("failed to get APIServer configuration: %w", err)
	}

	// Check if the additional CA is configured
	var updatedNamedCertificates []configv1.APIServerNamedServingCert
	caRemoved := false

	for _, cert := range apiServer.Spec.ServingCerts.NamedCertificates {
		if cert.ServingCertificate.Name != AdditionalServingCertSecretName {
			updatedNamedCertificates = append(updatedNamedCertificates, cert)
		} else {
			caRemoved = true
		}
	}

	// If the CA wasn't found, nothing to do
	if !caRemoved {
		return nil
	}

	// Update the APIServer configuration with the CA removed
	apiServer.Spec.ServingCerts.NamedCertificates = updatedNamedCertificates
	_, err = openshiftClient.ConfigV1().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update APIServer configuration: %w", err)
	}

	return nil
}

// getClusterClient returns a Kubernetes client for the managed cluster
func (r *ReconcileHiveConfig) getClusterClient(
	ctx context.Context,
	cluster *hivev1.ClusterDeployment,
) (*kubernetes.Clientset, error) {
	// Get the admin kubeconfig secret name from the ClusterDeployment
	secretRefName := cluster.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name

	// Get the admin kubeconfig secret
	kubeconfigSecret := &corev1.Secret{}
	err := r.hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      secretRefName,
	}, kubeconfigSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get admin kubeconfig secret %s: %w", secretRefName, err)
	}

	// Use the helper method to generate a client from the kubeconfig secret
	_, clientHolder, _, err := helpers.GenerateImportClientFromKubeConfigSecret(kubeconfigSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client from kubeconfig: %w", err)
	}

	// Return the Kubernetes client from the ClientHolder
	// Since KubeClient is of type kubernetes.Interface, we need to check if it's a *kubernetes.Clientset
	kubeClient, ok := clientHolder.KubeClient.(*kubernetes.Clientset)
	if !ok {
		return nil, fmt.Errorf("failed to convert KubeClient to *kubernetes.Clientset")
	}

	return kubeClient, nil
}

// getClusterOpenshiftClient returns an OpenShift client for the managed cluster
func (r *ReconcileHiveConfig) getClusterOpenshiftClient(
	ctx context.Context,
	cluster *hivev1.ClusterDeployment,
) (*openshiftclientset.Clientset, error) {
	// Get the admin kubeconfig secret name from the ClusterDeployment
	secretRefName := cluster.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name

	// Get the admin kubeconfig secret
	kubeconfigSecret := &corev1.Secret{}
	err := r.hubClientHolder.RuntimeClient.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      secretRefName,
	}, kubeconfigSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get admin kubeconfig secret %s: %w", secretRefName, err)
	}

	// Get the kubeconfig data from the secret
	kubeconfigData, ok := kubeconfigSecret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig key not found in secret %s", secretRefName)
	}

	// Load the kubeconfig
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create a client config from the kubeconfig
	clientConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create client config: %w", err)
	}

	// Create an OpenShift client from the client config
	openshiftClient, err := openshiftclientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenShift client: %w", err)
	}

	return openshiftClient, nil
}
