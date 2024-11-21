package helpers

import (
	"testing"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	csrNameReconcile = "csr-reconcile"
	clusterName      = "mycluster"
)

func Test_getClusterName(t *testing.T) {
	testCSR := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
			Labels: map[string]string{
				ClusterLabel: clusterName,
			},
		},
	}

	testCSRBadLabel := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
			Labels: map[string]string{
				"badLabel": clusterName,
			},
		},
	}

	testCSRNoLabel := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
		},
	}

	type args struct {
		csr *certificatesv1.CertificateSigningRequest
	}
	tests := []struct {
		name            string
		args            args
		wantClusterName string
	}{
		{
			name: "testCSR",
			args: args{
				csr: testCSR,
			},
			wantClusterName: clusterName,
		},
		{
			name: "testCSRBadLabel",
			args: args{
				csr: testCSRBadLabel,
			},
			wantClusterName: "",
		},
		{
			name: "testCSRNoLabel",
			args: args{
				csr: testCSRNoLabel,
			},
			wantClusterName: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotClusterName := GetClusterName(tt.args.csr); gotClusterName != tt.wantClusterName {
				t.Errorf("getClusterName() = %v, want %v", gotClusterName, tt.wantClusterName)
			}
		})
	}
}

func Test_getApproval(t *testing.T) {
	testCSRNoApproval := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
			Labels: map[string]string{
				ClusterLabel: clusterName,
			},
		},
	}

	testCSRApproved := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
			Labels: map[string]string{
				ClusterLabel: clusterName,
			},
		},
		Status: certificatesv1.CertificateSigningRequestStatus{
			Conditions: []certificatesv1.CertificateSigningRequestCondition{
				{Type: certificatesv1.CertificateApproved},
			},
		},
	}

	testCSRDenied := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: csrNameReconcile,
			Labels: map[string]string{
				ClusterLabel: clusterName,
			},
		},
		Status: certificatesv1.CertificateSigningRequestStatus{
			Conditions: []certificatesv1.CertificateSigningRequestCondition{
				{Type: certificatesv1.CertificateDenied},
			},
		},
	}

	type args struct {
		csr *certificatesv1.CertificateSigningRequest
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "testCSRNoApproval",
			args: args{
				csr: testCSRNoApproval,
			},
			want: "",
		},
		{
			name: "testCSRApproved",
			args: args{
				csr: testCSRApproved,
			},
			want: string(certificatesv1.CertificateApproved),
		},
		{
			name: "testCSRDenied",
			args: args{
				csr: testCSRDenied,
			},
			want: string(certificatesv1.CertificateDenied),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetApprovalType(tt.args.csr); got != tt.want {
				t.Errorf("GetApprovalType() = %v, want %v", got, tt.want)
			}
		})
	}
}
