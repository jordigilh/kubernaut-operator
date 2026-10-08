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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type options struct {
	mode         string
	address      string
	listen       string
	certDir      string
	expectedSelf string
	expectedPeer string
	once         bool
	interval     time.Duration
}

type identity struct {
	pair  tls.Certificate
	leaf  *x509.Certificate
	roots *x509.CertPool
}

func main() {
	opts := options{}
	flag.StringVar(&opts.mode, "mode", "", "probe mode: server or client")
	flag.StringVar(&opts.address, "address", "", "TLS server address for client mode")
	flag.StringVar(&opts.listen, "listen", ":8443", "listen address for server mode")
	flag.StringVar(&opts.certDir, "cert-dir", "/certs", "directory containing helper-generated certificates")
	flag.StringVar(&opts.expectedSelf, "expected-self", "", "expected SPIFFE ID of this workload")
	flag.StringVar(&opts.expectedPeer, "expected-peer", "", "expected SPIFFE ID of the peer")
	flag.BoolVar(&opts.once, "once", false, "perform one client handshake and exit")
	flag.DurationVar(&opts.interval, "interval", 5*time.Second, "client retry interval")
	flag.Parse()

	if opts.mode != "server" && opts.mode != "client" {
		fatalf("-mode must be server or client")
	}
	if opts.expectedSelf == "" || opts.expectedPeer == "" {
		fatalf("-expected-self and -expected-peer are required")
	}
	if opts.mode == "client" && opts.address == "" {
		fatalf("-address is required in client mode")
	}

	// spiffe-helper signals its child on SVID renewal. The probe reloads the
	// files on every client attempt, so it only needs to keep running after the
	// signal rather than handle the signal itself.
	signal.Ignore(syscall.SIGUSR1)

	if opts.mode == "server" {
		if err := serve(opts); err != nil {
			fatalf("server: %v", err)
		}
		return
	}
	if err := runClient(opts); err != nil {
		fatalf("client: %v", err)
	}
}

func serve(opts options) error {
	id, err := loadIdentity(opts)
	if err != nil {
		return err
	}

	config := &tls.Config{
		ClientAuth:             tls.RequireAndVerifyClientCert,
		ClientCAs:              id.roots,
		MinVersion:             tls.VersionTLS13,
		SessionTicketsDisabled: true,
		// Reload the helper-written certificate for every new connection so the
		// qualification proves that the serving side can survive SVID renewal.
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			current, loadErr := loadIdentity(opts)
			if loadErr != nil {
				return nil, loadErr
			}
			return &current.pair, nil
		},
		VerifyPeerCertificate: func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
				return errors.New("peer certificate chain was not verified")
			}
			return requireSPIFFEID(verifiedChains[0][0], opts.expectedPeer)
		},
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", opts.listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opts.listen, err)
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "mTLS-ok\n")
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("mTLS server ready; self=%s", opts.expectedSelf)
	err = server.Serve(tls.NewListener(listener, config))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runClient(opts options) error {
	var previousSerial string
	for {
		id, err := loadIdentity(opts)
		if err == nil {
			err = requestPeer(opts, id)
		}
		switch {
		case err == nil:
			serial := id.leaf.SerialNumber.String()
			if previousSerial != "" && previousSerial != serial {
				log.Printf("SVID serial rotated from %s to %s", previousSerial, serial)
			}
			previousSerial = serial
			log.Printf("mTLS handshake succeeded; self=%s peer=%s serial=%s", opts.expectedSelf, opts.expectedPeer, serial)
			if opts.once {
				return nil
			}
		case opts.once:
			return err
		default:
			log.Printf("mTLS handshake retry: %v", err)
		}

		timer := time.NewTimer(opts.interval)
		<-timer.C
	}
}

func requestPeer(opts options, id identity) error {
	config := &tls.Config{
		Certificates:           []tls.Certificate{id.pair},
		MinVersion:             tls.VersionTLS13,
		SessionTicketsDisabled: true,
		// Verification is performed against the SPIFFE bundle below because
		// SPIFFE identities use URI SANs rather than DNS names.
		InsecureSkipVerify: true, //nolint:gosec // custom SPIFFE bundle verification is mandatory below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPeerChain(rawCerts, id.roots, opts.expectedPeer)
		},
	}
	transport := &http.Transport{TLSClientConfig: config}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	defer transport.CloseIdleConnections()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+opts.address+"/", nil)
	if err != nil {
		return fmt.Errorf("creating peer request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("peer returned HTTP %s", response.Status)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("reading peer response: %w", err)
	}
	return nil
}

func loadIdentity(opts options) (identity, error) {
	certPEM, err := os.ReadFile(filepath(opts.certDir, "svid.pem"))
	if err != nil {
		return identity{}, fmt.Errorf("reading SVID certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath(opts.certDir, "svid_key.pem"))
	if err != nil {
		return identity{}, fmt.Errorf("reading SVID key: %w", err)
	}
	bundlePEM, err := os.ReadFile(filepath(opts.certDir, "svid_bundle.pem"))
	if err != nil {
		return identity{}, fmt.Errorf("reading SVID bundle: %w", err)
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return identity{}, fmt.Errorf("parsing SVID key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return identity{}, errors.New("SVID key pair contains no certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return identity{}, fmt.Errorf("parsing SVID certificate: %w", err)
	}
	if err := requireSPIFFEID(leaf, opts.expectedSelf); err != nil {
		return identity{}, fmt.Errorf("validating own SVID: %w", err)
	}

	roots := x509.NewCertPool()
	rootCount := 0
	for remaining := bundlePEM; len(remaining) > 0; {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return identity{}, fmt.Errorf("parsing SVID bundle: %w", parseErr)
		}
		roots.AddCert(certificate)
		rootCount++
	}
	if rootCount == 0 {
		return identity{}, errors.New("SVID bundle contains no certificates")
	}
	return identity{pair: pair, leaf: leaf, roots: roots}, nil
}

func verifyPeerChain(rawCerts [][]byte, roots *x509.CertPool, expectedID string) error {
	if len(rawCerts) == 0 {
		return errors.New("peer sent no certificate")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parsing peer certificate: %w", err)
	}
	intermediates := x509.NewCertPool()
	for _, raw := range rawCerts[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil {
			return fmt.Errorf("parsing peer intermediate: %w", parseErr)
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("verifying peer certificate chain: %w", err)
	}
	return requireSPIFFEID(leaf, expectedID)
}

func requireSPIFFEID(certificate *x509.Certificate, expected string) error {
	for _, uri := range certificate.URIs {
		if uri.String() == expected {
			return nil
		}
	}
	ids := make([]string, 0, len(certificate.URIs))
	for _, uri := range certificate.URIs {
		ids = append(ids, uri.String())
	}
	return fmt.Errorf("certificate SPIFFE IDs %v do not include %q", ids, expected)
}

func filepath(dir, name string) string {
	return strings.TrimRight(dir, "/") + "/" + name
}

func fatalf(format string, args ...interface{}) {
	log.Printf(format, args...)
	os.Exit(1)
}
