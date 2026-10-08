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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/jordigilh/kubernaut/pkg/shared/telemetry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
)

var _ = Describe("upstream OTLP telemetry TLS runtime", func() {
	It("IT-TELEMETRY-RUNTIME-001 [SC-8, SC-12, SC-13; ASVS v5.0.0-V12.2.1, v5.0.0-V12.3.1, v5.0.0-V12.3.2, v5.0.0-V12.3.4] completes a private-CA mTLS handshake", func() {
		caCert, caKey, caPEM := telemetryRuntimeCA()
		serverCert := telemetryRuntimeCertificate(caCert, caKey, false)
		clientCert, clientKey := telemetryRuntimeCertificateWithKey(caCert, caKey, true)

		trustPool := x509.NewCertPool()
		Expect(trustPool.AppendCertsFromPEM(caPEM)).To(BeTrue())
		received := make(chan struct{}, 1)
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			Expect(request.URL.Path).To(Equal("/v1/traces"))
			_, _ = io.Copy(io.Discard, request.Body)
			_ = request.Body.Close()
			response.WriteHeader(http.StatusOK)
			received <- struct{}{}
		}))
		server.TLS = &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    trustPool,
			MinVersion:   tls.VersionTLS12,
		}
		server.StartTLS()
		DeferCleanup(server.Close)

		tempDir, err := os.MkdirTemp("", "telemetry-runtime-test-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, tempDir)
		caPath := filepath.Join(tempDir, "ca.crt")
		clientCertPath := filepath.Join(tempDir, "tls.crt")
		clientKeyPath := filepath.Join(tempDir, "tls.key")
		Expect(os.WriteFile(caPath, caPEM, 0600)).To(Succeed())
		Expect(os.WriteFile(clientCertPath, clientCertPEM(clientCert), 0600)).To(Succeed())
		Expect(os.WriteFile(clientKeyPath, clientKeyPEM(clientKey), 0600)).To(Succeed())

		cfg := telemetry.Config{
			ServiceName: "telemetry-runtime-test",
			Endpoint:    server.Listener.Addr().String(),
		}
		setUpstreamTelemetryTLS(&cfg, caPath, clientCertPath, clientKeyPath)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown, err := telemetry.NewTracerProvider(ctx, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(shutdown).NotTo(BeNil())
		spanCtx, span := otel.Tracer("telemetry-runtime-test").Start(ctx, "private-ca-mtls")
		span.End()
		_ = spanCtx
		Expect(shutdown(ctx)).To(Succeed())

		select {
		case <-received:
		case <-ctx.Done():
			Fail("upstream OTLP exporter did not complete the private-CA/mTLS handshake")
		}
	})
})

// The upstream public Config type exposes TLS as a field whose type lives in
// its internal package. Reflection lets this operator-side qualification test
// populate that exported field without importing an upstream internal package.
func setUpstreamTelemetryTLS(config *telemetry.Config, caFile, certFile, keyFile string) {
	configValue := reflect.ValueOf(config).Elem()
	tlsField := configValue.FieldByName("TLS")
	Expect(tlsField.IsValid()).To(BeTrue())
	Expect(tlsField.CanSet()).To(BeTrue())
	tlsValue := reflect.New(tlsField.Type()).Elem()
	tlsValue.FieldByName("CAFile").SetString(caFile)
	tlsValue.FieldByName("CertFile").SetString(certFile)
	tlsValue.FieldByName("KeyFile").SetString(keyFile)
	tlsField.Set(tlsValue)
}

func telemetryRuntimeCA() (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "telemetry-runtime-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return template, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func telemetryRuntimeCertificate(ca *x509.Certificate, caKey *ecdsa.PrivateKey, client bool) tls.Certificate {
	certificate, _ := telemetryRuntimeCertificateWithKey(ca, caKey, client)
	return certificate
}

func telemetryRuntimeCertificateWithKey(ca *x509.Certificate, caKey *ecdsa.PrivateKey, client bool) (tls.Certificate, *ecdsa.PrivateKey) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "telemetry-runtime-peer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		clientKeyPEM(key),
	)
	Expect(err).NotTo(HaveOccurred())
	return certificate, key
}

func clientCertPEM(certificate tls.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
}

func clientKeyPEM(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}
