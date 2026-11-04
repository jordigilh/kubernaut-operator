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

package kind

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
)

type qualificationIngress struct {
	server             *http.Server
	portForward        *exec.Cmd
	portForwardMonitor *qualificationProcessMonitor
}

// qualificationProcessMonitor records a child process result without making
// readiness checks consume the result needed by cleanup.
type qualificationProcessMonitor struct {
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newQualificationProcessMonitor(command *exec.Cmd) *qualificationProcessMonitor {
	monitor := &qualificationProcessMonitor{done: make(chan struct{})}
	go func() {
		monitor.mu.Lock()
		monitor.err = command.Wait()
		monitor.mu.Unlock()
		close(monitor.done)
	}()
	return monitor
}

func (monitor *qualificationProcessMonitor) exitedError(description string) error {
	select {
	case <-monitor.done:
		monitor.mu.Lock()
		defer monitor.mu.Unlock()
		if monitor.err == nil {
			return fmt.Errorf("%s exited", description)
		}
		return fmt.Errorf("%s exited: %w", description, monitor.err)
	default:
		return nil
	}
}

func (monitor *qualificationProcessMonitor) stop(command *exec.Cmd) {
	select {
	case <-monitor.done:
		return
	default:
	}
	if command.Process != nil {
		if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_, _ = fmt.Fprintf(
				GinkgoWriter, "warning: stopping Console service port-forward failed: %v\n", err,
			) //nolint:errcheck
		}
	}
	<-monitor.done
}

// startQualificationIngress starts the loopback Console adapter only after a
// live kubectl port-forward process has successfully bound its upstream port.
func startQualificationIngress(ctx context.Context) (*qualificationIngress, error) {
	fixture, err := activeManualTLSFixture()
	if err != nil {
		return nil, fmt.Errorf("loading TLS fixture for the Console adapter: %w", err)
	}
	certificate, key, err := generateManualLeaf(
		time.Now().UTC(), fixture.caCertificate, fixture.caKey,
		qualificationConsoleHost, "qualification", "console",
	)
	if err != nil {
		return nil, fmt.Errorf("creating Console adapter certificate: %w", err)
	}
	serverCertificate, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return nil, fmt.Errorf("parsing Console adapter certificate: %w", err)
	}

	portForward := exec.CommandContext(
		ctx, "kubectl", "--context", clusterContext, "port-forward",
		"--address", "127.0.0.1", "service/kubernaut-console",
		"4180:4180", "-n", kubernautNamespace,
	)
	portForward.Stdout = GinkgoWriter
	portForward.Stderr = GinkgoWriter
	if err := portForward.Start(); err != nil {
		return nil, fmt.Errorf("starting Console service port-forward: %w", err)
	}
	portForwardMonitor := newQualificationProcessMonitor(portForward)
	stopPortForward := func() {
		portForwardMonitor.stop(portForward)
	}
	if err := pollUntilSuccess(ctx, 2*time.Minute, 500*time.Millisecond, func() error {
		if err := portForwardMonitor.exitedError("Console service port-forward"); err != nil {
			return err
		}
		dialer := &net.Dialer{Timeout: 2 * time.Second}
		connection, dialErr := dialer.DialContext(ctx, "tcp", "127.0.0.1:4180")
		if dialErr != nil {
			return dialErr
		}
		closeErr := connection.Close()
		if processErr := portForwardMonitor.exitedError("Console service port-forward"); processErr != nil {
			return processErr
		}
		return closeErr
	}); err != nil {
		stopPortForward()
		return nil, fmt.Errorf("waiting for Console service port-forward: %w", err)
	}

	target, err := url.Parse("http://127.0.0.1:4180")
	if err != nil {
		stopPortForward()
		return nil, fmt.Errorf("parsing Console adapter upstream: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = nil
	proxy.Rewrite = func(request *httputil.ProxyRequest) {
		request.SetURL(target)
		request.Out.Host = qualificationConsoleHost
		request.Out.Header.Set("X-Forwarded-Proto", "https")
		request.Out.Header.Set("X-Forwarded-Host", qualificationConsoleHost)
	}
	listener, err := (&net.ListenConfig{}).Listen(
		ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", qualificationConsoleHostPort),
	)
	if err != nil {
		stopPortForward()
		return nil, fmt.Errorf("binding Console adapter port %d: %w", qualificationConsoleHostPort, err)
	}
	server := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}
	adapter := &qualificationIngress{
		server:             server,
		portForward:        portForward,
		portForwardMonitor: portForwardMonitor,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{serverCertificate},
		MinVersion:   tls.VersionTLS12,
	})
	go func() {
		if serveErr := server.Serve(tlsListener); serveErr != nil && serveErr != http.ErrServerClosed {
			_, _ = fmt.Fprintf(GinkgoWriter, "Console adapter stopped: %v\n", serveErr) //nolint:errcheck
		}
	}()
	return adapter, nil
}

func (adapter *qualificationIngress) Close(ctx context.Context) error {
	if adapter == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	shutdownErr := adapter.server.Shutdown(shutdownCtx)
	if adapter.portForward != nil && adapter.portForwardMonitor != nil {
		adapter.portForwardMonitor.stop(adapter.portForward)
	}
	return shutdownErr
}

func qualificationHTTPClient(caPEM []byte, adapter *qualificationIngress) *http.Client {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		panic("qualification CA PEM did not contain a certificate")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil, splitErr
			}
			switch {
			case host == qualificationKeycloakService && port == "8443":
				address = fmt.Sprintf("127.0.0.1:%d", qualificationKeycloakHostPort)
			case host == qualificationConsoleHost && port == fmt.Sprint(qualificationConsoleHostPort):
				if adapter == nil {
					return nil, fmt.Errorf("console adapter is not running")
				}
				address = fmt.Sprintf("127.0.0.1:%d", qualificationConsoleHostPort)
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func qualificationResponseBody(response *http.Response) (string, error) {
	if response == nil {
		return "", fmt.Errorf("qualification response is nil")
	}
	body, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil {
		return "", fmt.Errorf("reading qualification response: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("closing qualification response: %w", closeErr)
	}
	return string(body), nil
}

func qualificationLocation(response *http.Response) (string, error) {
	location := strings.TrimSpace(response.Header.Get("Location"))
	if location == "" {
		return "", fmt.Errorf("HTTP %d response did not include Location", response.StatusCode)
	}
	return location, nil
}
