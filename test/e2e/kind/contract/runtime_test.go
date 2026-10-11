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

package contract

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubernautv1alpha2 "github.com/jordigilh/kubernaut-operator/api/v1alpha2"
	"github.com/jordigilh/kubernaut-operator/internal/resources"
)

var _ = Describe("contract image serving-certificate lifecycle", func() {
	It("CONTRACT-TLS-517-001 reloads projected serving material and rejects the replaced root", func() {
		directory := GinkgoT().TempDir()
		mount := filepath.Join(directory, "tls")
		Expect(os.Mkdir(mount, 0700)).To(Succeed())
		originalCA, originalLeaf := contractServingMaterial(nil)
		writeProjectedTLSGeneration(mount, "original", originalLeaf)
		Expect(os.Symlink("original", filepath.Join(mount, "..data"))).To(Succeed())
		for _, name := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
			Expect(os.Symlink(filepath.Join("..data", name), filepath.Join(mount, name))).To(Succeed())
		}
		address := startContractEntrypoint(directory, mount)
		Eventually(func() ([]byte, error) {
			return servedCertificate(address, originalCA)
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(certificateDER(originalLeaf)))

		By("replacing the Kubernetes-style projected generation without restarting the Pod")
		replacementCA, replacementLeaf := contractServingMaterial(nil)
		writeProjectedTLSGeneration(mount, "replacement", replacementLeaf)
		Expect(os.Symlink("replacement", filepath.Join(mount, "..data-next"))).To(Succeed())
		Expect(os.Rename(filepath.Join(mount, "..data-next"), filepath.Join(mount, "..data"))).To(Succeed())
		Eventually(func() ([]byte, error) {
			return servedCertificate(address, replacementCA)
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(certificateDER(replacementLeaf)))
		_, err := servedCertificate(address, originalCA)
		Expect(err).To(HaveOccurred(), "the old root must not authenticate the replacement serving certificate")
	})

	It("CONTRACT-TLS-517-002 starts TLS when projected material arrives after the health process", func() {
		directory := GinkgoT().TempDir()
		mount := filepath.Join(directory, "tls")
		Expect(os.Mkdir(mount, 0700)).To(Succeed())
		address := startContractEntrypoint(directory, mount)
		ca, leaf := contractServingMaterial(nil)
		Consistently(func() bool {
			_, err := servedCertificate(address, ca)
			return err != nil
		}, time.Second, 100*time.Millisecond).Should(BeTrue())
		for _, name := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
			Expect(os.WriteFile(filepath.Join(mount, name), leaf.Data[name], 0600)).To(Succeed())
		}
		Eventually(func() ([]byte, error) {
			return servedCertificate(address, ca)
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(certificateDER(leaf)))
	})

	It("CONTRACT-TLS-517-003 serves a reissued leaf even when the trusted CA does not change", func() {
		directory := GinkgoT().TempDir()
		mount := filepath.Join(directory, "tls")
		Expect(os.Mkdir(mount, 0700)).To(Succeed())
		ca, originalLeaf := contractServingMaterial(nil)
		writeProjectedTLSGeneration(mount, "original", originalLeaf)
		Expect(os.Symlink("original", filepath.Join(mount, "..data"))).To(Succeed())
		for _, name := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
			Expect(os.Symlink(filepath.Join("..data", name), filepath.Join(mount, name))).To(Succeed())
		}
		address := startContractEntrypoint(directory, mount)
		Eventually(func() ([]byte, error) {
			return servedCertificate(address, ca)
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(certificateDER(originalLeaf)))

		retainedCA, reissuedLeaf := contractServingMaterial(map[string]*corev1.Secret{ca.Name: ca})
		Expect(retainedCA.Data["ca.crt"]).To(Equal(ca.Data["ca.crt"]))
		Expect(certificateDER(reissuedLeaf)).NotTo(Equal(certificateDER(originalLeaf)))
		writeProjectedTLSGeneration(mount, "reissued", reissuedLeaf)
		Expect(os.Symlink("reissued", filepath.Join(mount, "..data-next"))).To(Succeed())
		Expect(os.Rename(filepath.Join(mount, "..data-next"), filepath.Join(mount, "..data"))).To(Succeed())
		Eventually(func() ([]byte, error) {
			return servedCertificate(address, ca)
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(certificateDER(reissuedLeaf)))
	})
})

// Use real production cryptography; only the unrelated health process is stubbed.
func contractServingMaterial(existing map[string]*corev1.Secret) (*corev1.Secret, *corev1.Secret) {
	kn := &kubernautv1alpha2.Kubernaut{
		ObjectMeta: metav1.ObjectMeta{Name: kubernautv1alpha2.SingletonName, Namespace: "contract-runtime"},
		Spec: kubernautv1alpha2.KubernautSpec{TLS: kubernautv1alpha2.TLSConfigSpec{
			Mode:                  kubernautv1alpha2.TLSModeDevelopmentSelfSigned,
			DevelopmentSelfSigned: &kubernautv1alpha2.DevelopmentSelfSignedTLSConfig{},
		}},
	}
	secrets, err := resources.DevelopmentSelfSignedTLSSecrets(kn, existing, time.Now().UTC())
	Expect(err).NotTo(HaveOccurred())
	for _, secret := range secrets {
		if secret.Name == resources.DataStorageTLSSecretName {
			return secrets[0], secret
		}
	}
	Fail("the production TLS builder did not generate DataStorage serving material")
	return nil, nil
}

func writeProjectedTLSGeneration(mount, generation string, leaf *corev1.Secret) {
	directory := filepath.Join(mount, generation)
	Expect(os.Mkdir(directory, 0700)).To(Succeed())
	for _, name := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
		Expect(os.WriteFile(filepath.Join(directory, name), leaf.Data[name], 0600)).To(Succeed())
	}
}

func startContractEntrypoint(directory, mount string) string {
	_, err := exec.LookPath("openssl")
	Expect(err).NotTo(HaveOccurred(), "the runtime contract requires a real OpenSSL executable")
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	address := listener.Addr().String()
	Expect(listener.Close()).To(Succeed())
	source, err := os.ReadFile("entrypoint.sh")
	Expect(err).NotTo(HaveOccurred())
	// Translate only the container's fixed mount and listening address for host
	// isolation. Certificate/reload/control-flow logic is executed unchanged.
	script := strings.ReplaceAll(string(source), "/etc/tls", mount)
	script = strings.ReplaceAll(script, "-accept 8443", "-accept "+address)
	scriptPath := filepath.Join(directory, "entrypoint.sh")
	root, err := os.OpenRoot(directory)
	Expect(err).NotTo(HaveOccurred())
	defer func() { Expect(root.Close()).To(Succeed()) }()
	Expect(root.WriteFile("entrypoint.sh", []byte(script), 0600)).To(Succeed())
	tools := filepath.Join(directory, "tools")
	Expect(root.Mkdir("tools", 0700)).To(Succeed())
	Expect(root.WriteFile("tools/darkhttpd", []byte("#!/bin/sh\nexec sleep 60\n"), 0600)).To(Succeed())
	Expect(root.Chmod("tools/darkhttpd", 0700)).To(Succeed())
	//nolint:gosec // only this repository's fixture in our confined temporary root is executed
	command := exec.CommandContext(ctx, "sh", scriptPath)
	command.Env = append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	command.WaitDelay = 3 * time.Second
	Expect(command.Start()).To(Succeed())
	DeferCleanup(func() {
		cancel()
		err := command.Wait()
		var exitError *exec.ExitError
		Expect(err == nil || errors.Is(err, context.Canceled) || errors.As(err, &exitError)).To(BeTrue())
	})
	return address
}

func certificateDER(secret *corev1.Secret) []byte {
	block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
	Expect(block.Type).To(Equal("CERTIFICATE"))
	return block.Bytes
}

func servedCertificate(address string, ca *corev1.Secret) ([]byte, error) {
	roots := x509.NewCertPool()
	Expect(roots.AppendCertsFromPEM(ca.Data["ca.crt"])).To(BeTrue())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: time.Second},
		Config:    &tls.Config{RootCAs: roots, ServerName: "data-storage-service", MinVersion: tls.VersionTLS12},
	}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	defer func() { Expect(connection.Close()).To(Succeed()) }()
	tlsConnection, ok := connection.(*tls.Conn)
	Expect(ok).To(BeTrue(), "the real TLS dialer must return a TLS connection")
	return tlsConnection.ConnectionState().PeerCertificates[0].Raw, nil
}
