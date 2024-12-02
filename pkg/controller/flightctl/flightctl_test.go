package flightctl

import (
	"testing"
	"time"
)

// TODO: add test cases for `IsFlightCtlEnabled` and `IsManagedClusterAFlightctlDevice`
func TestFlightCtl_IsFlightCtlEnabled(t *testing.T) {

}

// TODO: add test cases for `ApplyRepository`
func TestFlightCtl_ApplyRepository(t *testing.T) {

}

// TODO: add test cases for `IsManagedClusterAFlightctlDevice`
func TestFlightCtl_IsManagedClusterAFlightctlDevice(t *testing.T) {

}

func TestFlightCtl_tokenCloseToExpire(t *testing.T) {
	testcases := []struct {
		token        string
		timeDuration time.Duration
		expected     bool
	}{
		{
			token:        "eyJhbGciOiJSUzI1NiIsImtpZCI6Im5DZE1nUC0wQnA5QjJKQS1PQ3lOdDdqZG14SUhrN1lXZU1LMVhtOERUTHcifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjIl0sImV4cCI6MTczNDAxNjcwMSwiaWF0IjoxNzMzMTUyNzAxLCJpc3MiOiJodHRwczovL2t1YmVybmV0ZXMuZGVmYXVsdC5zdmMiLCJrdWJlcm5ldGVzLmlvIjp7Im5hbWVzcGFjZSI6ImRlZmF1bHQiLCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoiZGVmYXVsdCIsInVpZCI6IjZjZGVhN2NkLTAxZTYtNDVhMy1iMTZjLTk5ZTEyMzExNTMyYyJ9fSwibmJmIjoxNzMzMTUyNzAxLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpkZWZhdWx0In0.VA620a1pJ7NH21ttPqngB0BHmCWlfpoapI4wexnFqWnTzlljyKwuEauSLkTACWelt0Rs5x5G7f2fy37pVCaQZpzzJZlGy1RNxGKMJ7tDCiNmoGCDjZcqBwfaJqRYAGpHvVLc2gwhdnm_-TjeZNc2kpxeo9o3XABFHWJCS5IPmkEny1nh71Pb77CDizNb_K0tu-UfqDVtOLXCTMDhcJ81B9OoqMVew7_omU7c5A-mjLMBlxD63KrolsRlYgE8lT9NkEM5Kolg0wJKaiTofxVTfzF35qkFQv0nG6T2vqHH0hFjDVTzCDG2zhreXUlzQB8x1a2dWYwNaYDNJbDsKGP2NSBZaq_FNjDDpKCsiWZTEEP1sDOrqnXa-s_V1wNXghqz9Fdio-VO-H0MqPGjHmtg1xcWahSzscdhb7r97mNpyW7cLQm2guVRhuEyimaiqW_oBIRfTX4wirFK12XEHbyMoIAYZOL-H1QguXA4HftH9vxQsAkv5gr7ggLAGCRZSuyK_AsksJiIAgtDF2mPHEtnK_JStq0X0ky3--1HghFjLhUmZIZni6-tnXgazH8KvgSSc4mtfaWOmIglcYOgS63H4mb0_ZTSIYnlaVh8sOWNEF3ZxSxzzeCmquxWwCMdGPG6WalUS05AG2UHlpQiADjLb4mfFrFyWxOrX93ud9c6P34",
			timeDuration: 7 * 24 * time.Hour,
			expected:     false,
		},
		{
			token:        "eyJhbGciOiJSUzI1NiIsImtpZCI6Im5DZE1nUC0wQnA5QjJKQS1PQ3lOdDdqZG14SUhrN1lXZU1LMVhtOERUTHcifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjIl0sImV4cCI6MTczMzE2NzE4MCwiaWF0IjoxNzMzMTUyNzgwLCJpc3MiOiJodHRwczovL2t1YmVybmV0ZXMuZGVmYXVsdC5zdmMiLCJrdWJlcm5ldGVzLmlvIjp7Im5hbWVzcGFjZSI6ImRlZmF1bHQiLCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoiZGVmYXVsdCIsInVpZCI6IjZjZGVhN2NkLTAxZTYtNDVhMy1iMTZjLTk5ZTEyMzExNTMyYyJ9fSwibmJmIjoxNzMzMTUyNzgwLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpkZWZhdWx0In0.kB4CUUTdCY2hCgDFdEX4pWT3ZPmYxzYxohzeir7yJJXuKvvA6wPTRxNdbLFQBMC4knh_Dww7uTQhSJiYra6Q8CJrK8lIgp_H5fksYb3atNaSJJT_3qYTsR8l2QQqDhZcjztIQqnipcYYy_UQYPVujeg69P2sy5LtfwsqUtZed3vS8kX2A0rQcG2lzOhvHpwAbLXJwsiaB6y3h_zgncT2DpiHzN9XxaT4au8W1wwdcEECgkbHp0C8QzXPDcp0_hjI2987ENfp-9cW2czbSWxQprIPM4hqYbOdDdk1Cqb0FNjHAEL6giBkZOHd7NE4rcRD4V6MpWYvPrhP7q8wfpNZkbQ2WANMDDWyLow-eMWao9BZLcSu5ymdl_5A1xdUfgj4h0EIjXTrJV8o58kKq60JYRIbpBAOwyXoRsydyFFjbjgz1IuQxaP3v4zntSRqxmTgW1biWw-zwh_csbVgYlV-lna79htcfBZ5_HYITLXAlyNIC9eB7NAXzlFM3GA-MP4fpVhxpfl_aDwHV5-tx_F_9LgNBUPvhQPCZjy4k9DcOS6fHlSwOnrzOq2ECG7saKinmLnMRkfOBoKquXCDT4UZXBPNl5mzapi1iN5TEAavnVKbKdHjV-LYkLmydg5G6NSuOf408_y4NSnqodfziKRv6Gy7fzfyKHxQAwuZT6nw3x4",
			timeDuration: 7 * 24 * time.Hour,
			expected:     true,
		},
	}

	for _, tc := range testcases {
		actual, err := tokenCloseToExpire(tc.token, tc.timeDuration)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if actual != tc.expected {
			t.Errorf("expected: %v, actual: %v", tc.expected, actual)
		}
	}
}
