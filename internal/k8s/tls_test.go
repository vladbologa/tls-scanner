package k8s

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewTLSSecurityProfileFromTypeModern(t *testing.T) {
	t.Parallel()

	got, err := NewTLSSecurityProfileFromType("Modern")
	if err != nil {
		t.Fatalf("NewTLSSecurityProfileFromType() error = %v", err)
	}
	if got.APIServer.Type != "Modern" {
		t.Errorf("APIServer.Type = %q, want Modern", got.APIServer.Type)
	}
	if got.IngressController.Type != "Modern" {
		t.Errorf("IngressController.Type = %q, want Modern", got.IngressController.Type)
	}
}

func TestNewTLSSecurityProfileFromTypeInvalid(t *testing.T) {
	t.Parallel()

	if _, err := NewTLSSecurityProfileFromType("Custom"); err == nil {
		t.Fatal("expected error for unsupported profile type")
	}
}

func TestExtractAPIServerTLSNilProfile(t *testing.T) {
	t.Parallel()

	apiserver := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       configv1.APIServerSpec{},
	}

	got := extractAPIServerTLS(apiserver)
	if got.Type != "Default" {
		t.Errorf("Type = %q, want %q", got.Type, "Default")
	}
	if got.MinTLSVersion == "" {
		t.Error("MinTLSVersion should not be empty for default profile")
	}
	if len(got.Ciphers) == 0 {
		t.Error("Ciphers should not be empty for default profile")
	}
}

func TestExtractAPIServerTLSCustomProfile(t *testing.T) {
	t.Parallel()

	apiserver := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-CHACHA20-POLY1305"},
						MinTLSVersion: configv1.VersionTLS12,
					},
				},
			},
		},
	}

	got := extractAPIServerTLS(apiserver)
	if got.Type != "Custom" {
		t.Errorf("Type = %q, want %q", got.Type, "Custom")
	}
	if got.MinTLSVersion != "VersionTLS12" {
		t.Errorf("MinTLSVersion = %q, want %q", got.MinTLSVersion, "VersionTLS12")
	}
	if len(got.Ciphers) != 2 {
		t.Fatalf("expected 2 ciphers, got %d", len(got.Ciphers))
	}
}

func TestExtractAPIServerTLSPredefinedProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		profileType configv1.TLSProfileType
		wantType    string
	}{
		{"Old", configv1.TLSProfileOldType, "Old"},
		{"Intermediate", configv1.TLSProfileIntermediateType, "Intermediate"},
		{"Modern", configv1.TLSProfileModernType, "Modern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiserver := &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: tt.profileType,
					},
				},
			}

			got := extractAPIServerTLS(apiserver)
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}

			expected := configv1.TLSProfiles[tt.profileType]
			if got.MinTLSVersion != string(expected.MinTLSVersion) {
				t.Errorf("MinTLSVersion = %q, want %q", got.MinTLSVersion, expected.MinTLSVersion)
			}
			if len(got.Ciphers) != len(expected.Ciphers) {
				t.Errorf("Ciphers count = %d, want %d", len(got.Ciphers), len(expected.Ciphers))
			}
		})
	}
}
