package flightctl

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	flightctlapiv1 "github.com/flightctl/flightctl/lib/apipublic/v1alpha1"
	flightctlcli "github.com/flightctl/flightctl/lib/cli"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const (
	FlightCtlNamespace      = "flightctl"
	FlightCtlInternalServer = "https://flightctl-api.flightctl.svc.cluster.local:3443"
)

//go:embed manifests
var FlightCtlManifestFiles embed.FS

// The flightctl-client's service account token has 2 usages:
// 1. delivered to the devices, used to access the agent-registration to get klusterlet manifests used for registration.
// 2. on the hub side, used for the import-controller to apply the flightctl's Repository resources and get devices.
var files = []string{
	"manifests/clusterrole.yml",
	"manifests/clusterrolebinding_agentregistration.yml",
	"manifests/clusterrolebinding_flightctl.yml",
	"manifests/serviceaccount.yml",
	"manifests/networkpolicy.yml",
}

type FlightCtler interface {
	IsFlightCtlEnabled(ctx context.Context) (bool, error)
	ApplyRepository(ctx context.Context) error
	IsManagedClusterAFlightctlDevice(ctx context.Context, managedClusterName string) (bool, error)
}

func NewFlightCtl(clientHolder *helpers.ClientHolder, clusterIngressDomain string) FlightCtler {
	return &FlightCtl{
		flightctlServer:         FlightCtlInternalServer,
		agentRegistrationServer: "https://agent-registration-multicluster-engine." + clusterIngressDomain,
		clientHolder:            clientHolder,
	}
}

type FlightCtl struct {
	clientHolder            *helpers.ClientHolder
	flightctlServer         string
	agentRegistrationServer string
	cachedToken             string
	cachedCA                string
}

var _ FlightCtler = &FlightCtl{}

func (f *FlightCtl) IsFlightCtlEnabled(ctx context.Context) (bool, error) {
	// If `flightctl` ns is not created, means the flightctl is not enabled.
	ns := &corev1.Namespace{}
	err := f.clientHolder.RuntimeClient.Get(ctx, types.NamespacedName{
		Name: FlightCtlNamespace,
	}, ns)

	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (f *FlightCtl) ApplyRepository(ctx context.Context) error {
	token, err := f.getFlightCtlClientServiceAccountToken(ctx)
	if err != nil {
		return err
	}

	ca, err := f.getAgentRegistrationCA()
	if err != nil {
		return err
	}

	expectedRepository := &flightctlapiv1.Repository{
		ApiVersion: "v1alpha1",
		Kind:       "Repository",
		Metadata: flightctlapiv1.ObjectMeta{
			// Note: In the fleets' `httpRef.repository` field, the name is `acm-registration`.
			// See details in: https://github.com/flightctl/flightctl/blob/main/docs/user/registering-microshift-devices-acm.md
			Name: ptr.To("acm-registration"),
		},
		Spec: flightctlapiv1.RepositorySpec{},
	}
	err = expectedRepository.Spec.MergeHttpRepoSpec(flightctlapiv1.HttpRepoSpec{
		Type: flightctlapiv1.Http,
		Url:  f.agentRegistrationServer,
		HttpConfig: flightctlapiv1.HttpConfig{
			Token: &token,
			CaCrt: &ca,
		},
		ValidationSuffix: ptr.To("/agent-registration"),
	})
	if err != nil {
		return err
	}

	return flightctlcli.ApplyRepository(ctx, token, f.flightctlServer, expectedRepository)
}

func (f *FlightCtl) IsManagedClusterAFlightctlDevice(ctx context.Context, managedClusterName string) (bool, error) {
	token, err := f.getFlightCtlClientServiceAccountToken(ctx)
	if err != nil {
		return false, err
	}

	response, err := flightctlcli.GetDevice(ctx, token, f.flightctlServer, managedClusterName)
	if err != nil {
		return false, err
	}

	if response.HTTPResponse.StatusCode == http.StatusNotFound {
		return false, nil
	}

	if response.HTTPResponse.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to get device %s, status code: %d", managedClusterName, response.HTTPResponse.StatusCode)
	}

	return true, nil
}

func (f *FlightCtl) getFlightCtlClientServiceAccountToken(ctx context.Context) (string, error) {
	if f.cachedToken != "" {
		// check if the cached token is close to expire, use 7 days as the threshold
		if closeToExpire, err := tokenCloseToExpire(f.cachedToken, 7*24*time.Hour); err != nil {
			return "", err
		} else if !closeToExpire { // if not close to expire, return the cached token
			return f.cachedToken, nil
		}
	}

	// Create token request for the service account
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To[int64](10 * 24 * 60 * 60), // Give 10 days for the token to expire
		},
	}

	// Get the token using TokenRequest API
	tokenResponse, err := f.clientHolder.KubeClient.CoreV1().ServiceAccounts(FlightCtlNamespace).
		CreateToken(ctx, "flightctl-client", tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create token: %v", err)
	}

	f.cachedToken = tokenResponse.Status.Token
	return f.cachedToken, nil
}

func (f *FlightCtl) getAgentRegistrationCA() (string, error) {
	if f.cachedCA != "" {
		return f.cachedCA, nil
	}

	caData, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return "", fmt.Errorf("failed to read service account CA: %v", err)
	}

	f.cachedCA = base64.StdEncoding.EncodeToString(caData)
	return f.cachedCA, nil
}

func tokenCloseToExpire(token string, timeDuration time.Duration) (bool, error) {
	// Split the token into parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid token format")
	}

	// Decode the claims (second part of the token)
	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("failed to decode token claims: %v", err)
	}

	// Parse the claims
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(claimBytes, &claims); err != nil {
		return false, fmt.Errorf("failed to parse token claims: %v", err)
	}

	// Check if token will expire within the given duration
	expirationTime := time.Unix(claims.Exp, 0)
	timeUntilExpiration := time.Until(expirationTime)
	return timeUntilExpiration <= timeDuration, nil
}
