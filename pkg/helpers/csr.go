package helpers

import certificatesv1 "k8s.io/api/certificates/v1"

const (
	ClusterLabel = "open-cluster-management.io/cluster-name"
)

func GetClusterName(csr *certificatesv1.CertificateSigningRequest) (clusterName string) {
	for label, v := range csr.GetObjectMeta().GetLabels() {
		if label == ClusterLabel {
			clusterName = v
		}
	}
	return clusterName
}

func GetApprovalType(csr *certificatesv1.CertificateSigningRequest) string {
	if csr.Status.Conditions == nil {
		return ""
	}
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved || c.Type == certificatesv1.CertificateDenied {
			return string(c.Type)
		}
	}
	return ""
}

// check whether a CSR is in terminal state
func IsCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}
