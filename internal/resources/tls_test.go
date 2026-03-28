/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestAuthWebhookTLSSecret_GeneratesValidCerts(t *testing.T) {
	kn := testKubernaut()
	secret, caBundle, err := AuthWebhookTLSSecret(kn)
	if err != nil {
		t.Fatalf("AuthWebhookTLSSecret() error: %v", err)
	}

	if secret.Name != "authwebhook-tls" {
		t.Errorf("secret name = %q, want %q", secret.Name, "authwebhook-tls")
	}
	if secret.Namespace != "kubernaut-system" {
		t.Errorf("secret namespace = %q, want %q", secret.Namespace, "kubernaut-system")
	}

	if len(caBundle) == 0 {
		t.Fatal("caBundle should not be empty")
	}
	if len(secret.Data["tls.crt"]) == 0 {
		t.Fatal("tls.crt should not be empty")
	}
	if len(secret.Data["tls.key"]) == 0 {
		t.Fatal("tls.key should not be empty")
	}
	if len(secret.Data["ca.crt"]) == 0 {
		t.Fatal("ca.crt should not be empty")
	}
}

func TestAuthWebhookTLSSecret_CAParseable(t *testing.T) {
	kn := testKubernaut()
	_, caBundle, err := AuthWebhookTLSSecret(kn)
	if err != nil {
		t.Fatalf("AuthWebhookTLSSecret() error: %v", err)
	}

	block, _ := pem.Decode(caBundle)
	if block == nil {
		t.Fatal("caBundle is not valid PEM")
	}

	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}
	if !ca.IsCA {
		t.Error("CA certificate should have IsCA=true")
	}
	if ca.Subject.CommonName != "kubernaut-authwebhook-ca" {
		t.Errorf("CA CN = %q, want %q", ca.Subject.CommonName, "kubernaut-authwebhook-ca")
	}
}

func TestAuthWebhookTLSSecret_ServerCertDNSNames(t *testing.T) {
	kn := testKubernaut()
	secret, _, err := AuthWebhookTLSSecret(kn)
	if err != nil {
		t.Fatalf("AuthWebhookTLSSecret() error: %v", err)
	}

	block, _ := pem.Decode(secret.Data["tls.crt"])
	if block == nil {
		t.Fatal("tls.crt is not valid PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse server cert: %v", err)
	}

	expectedDNS := []string{
		"authwebhook-service",
		"authwebhook-service.kubernaut-system",
		"authwebhook-service.kubernaut-system.svc",
		"authwebhook-service.kubernaut-system.svc.cluster.local",
	}

	dnsSet := make(map[string]bool, len(cert.DNSNames))
	for _, d := range cert.DNSNames {
		dnsSet[d] = true
	}

	for _, expected := range expectedDNS {
		if !dnsSet[expected] {
			t.Errorf("server cert missing DNS SAN %q, has: %v", expected, cert.DNSNames)
		}
	}
}

func TestAuthWebhookTLSSecret_ServerCertSignedByCA(t *testing.T) {
	kn := testKubernaut()
	secret, caBundle, err := AuthWebhookTLSSecret(kn)
	if err != nil {
		t.Fatalf("AuthWebhookTLSSecret() error: %v", err)
	}

	caBlock, _ := pem.Decode(caBundle)
	ca, _ := x509.ParseCertificate(caBlock.Bytes)

	serverBlock, _ := pem.Decode(secret.Data["tls.crt"])
	serverCert, _ := x509.ParseCertificate(serverBlock.Bytes)

	pool := x509.NewCertPool()
	pool.AddCert(ca)

	_, err = serverCert.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "authwebhook-service.kubernaut-system.svc",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		t.Errorf("server cert should verify against CA: %v", err)
	}
}
