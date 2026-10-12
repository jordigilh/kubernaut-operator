//go:build integration

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"
)

var _ = Describe("actual bootstrap webhook protocol", Label("integration"), func() {
	It("IT-AUDIT-515-TLS-001 [SC-5, SC-8, CM-6]: negotiates only HTTP/1.1 by default, supports explicit HTTP/2 opt-in and rejects TLS 1.1", func() {
		certificate, roots := bootstrapTestCertificate()
		for _, enabled := range []bool{false, true} {
			options := buildManagerOptions(&operatorFlags{enableHTTP2: enabled}, metricsserver.Options{BindAddress: "0"})
			server := options.WebhookServer.(*webhookserver.DefaultServer)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			address := listener.Addr().String()
			server.Options.Host = "127.0.0.1"
			server.Options.Port = listener.Addr().(*net.TCPAddr).Port
			Expect(listener.Close()).To(Succeed())
			server.Options.TLSOpts = append(server.Options.TLSOpts, func(config *tls.Config) {
				config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &certificate, nil }
			})
			serverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			serverErrors := make(chan error, 1)
			go func() { serverErrors <- server.Start(serverCtx) }()
			DeferCleanup(func() {
				cancel()
				Eventually(serverErrors, 10*time.Second, 50*time.Millisecond).Should(Receive(BeNil()))
			})
			Eventually(func() error { return server.StartedChecker()(nil) }, 10*time.Second, 50*time.Millisecond).Should(Succeed())
			connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", address, &tls.Config{
				RootCAs: roots, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12,
			})
			Expect(err).NotTo(HaveOccurred())
			protocol := "http/1.1"
			if enabled {
				protocol = "h2"
			}
			Expect(connection.ConnectionState().NegotiatedProtocol).To(Equal(protocol))
			Expect(connection.Close()).To(Succeed())
			oldConnection, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", address, &tls.Config{
				RootCAs: roots, MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11, //nolint:gosec // deliberately rejected protocol probe
			})
			if oldConnection != nil {
				Expect(oldConnection.Close()).To(Succeed())
			}
			Expect(err).To(HaveOccurred(), "the actual webhook must reject the obsolete client protocol")
		}
	})
})

var _ = Describe("actual bootstrap metrics protocol", Label("integration"), func() {
	It("IT-AUDIT-515-TLS-002 [SC-5, SC-8, AC-3, CM-6]: negotiates the production TLS policy and retains metrics authentication", func() {
		certificate, roots := bootstrapTestCertificate()
		for _, enabled := range []bool{false, true} {
			options, watcher, err := buildMetricsServerOptions(&operatorFlags{
				metricsAddr: "127.0.0.1:0", secureMetrics: true, enableHTTP2: enabled,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(watcher).To(BeNil())
			options.TLSOpts = append(options.TLSOpts, func(config *tls.Config) {
				config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &certificate, nil }
			})
			server, err := metricsserver.NewServer(options, &rest.Config{Host: "https://unit.invalid"}, &http.Client{})
			Expect(err).NotTo(HaveOccurred())
			addressReader, ok := server.(interface{ GetBindAddr() string })
			Expect(ok).To(BeTrue())
			serverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			serverErrors := make(chan error, 1)
			go func() { serverErrors <- server.Start(serverCtx) }()
			DeferCleanup(func() {
				cancel()
				Eventually(serverErrors, 10*time.Second, 50*time.Millisecond).Should(Receive(BeNil()))
			})
			Eventually(addressReader.GetBindAddr, 10*time.Second, 50*time.Millisecond).
				Should(MatchRegexp(`^127\.0\.0\.1:[1-9][0-9]{0,4}$`))
			address := addressReader.GetBindAddr()
			connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", address, &tls.Config{
				RootCAs: roots, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12,
			})
			Expect(err).NotTo(HaveOccurred())
			protocol := "http/1.1"
			if enabled {
				protocol = "h2"
			}
			Expect(connection.ConnectionState().NegotiatedProtocol).To(Equal(protocol))
			Expect(connection.Close()).To(Succeed())
			oldConnection, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", address, &tls.Config{
				RootCAs: roots, MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11, //nolint:gosec // rejected-protocol probe
			})
			if oldConnection != nil {
				Expect(oldConnection.Close()).To(Succeed())
			}
			Expect(err).To(HaveOccurred(), "the metrics listener must reject TLS 1.1")
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: enabled}
			DeferCleanup(transport.CloseIdleConnections)
			httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			response, err := httpClient.Get("https://" + address + "/metrics")
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Body.Close()).To(Succeed())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized), "a TLS-only test must not disable the production filter")
		}
	})
})

func bootstrapTestCertificate() (tls.Certificate, *x509.CertPool) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	certificate, err := x509.ParseCertificate(der)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}
