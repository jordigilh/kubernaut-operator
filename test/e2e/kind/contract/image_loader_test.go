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
	"os"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck

	e2eimage "github.com/jordigilh/kubernaut-operator/test/e2e/internal/image"
)

var _ = Describe("Kind image loading", func() {
	It("exports one platform before loading the archive into Kind", func() {
		const platform = "linux/amd64"
		previousPlatform, wasSet := os.LookupEnv(e2eimage.PlatformEnvironment)
		Expect(os.Setenv(e2eimage.PlatformEnvironment, platform)).To(Succeed())
		DeferCleanup(func() {
			if wasSet {
				Expect(os.Setenv(e2eimage.PlatformEnvironment, previousPlatform)).To(Succeed())
				return
			}
			Expect(os.Unsetenv(e2eimage.PlatformEnvironment)).To(Succeed())
		})

		var calls [][]string
		run := func(_ context.Context, command string, args ...string) (string, error) {
			calls = append(calls, append([]string{command}, args...))
			return "", nil
		}

		Expect(e2eimage.Load(context.Background(), run, "docker", "postgres:16.4", "kind-test")).To(Succeed())
		Expect(calls).To(HaveLen(2))
		Expect(calls[0][:6]).To(Equal([]string{
			"docker", "image", "save", "--platform", platform, "--output",
		}))
		archivePath := calls[0][6]
		Expect(archivePath).To(HaveSuffix("/image.tar"))
		Expect(calls[0][7]).To(Equal("postgres:16.4"))
		Expect(calls[1]).To(Equal([]string{
			"kind", "load", "image-archive", archivePath, "--name", "kind-test",
		}))
	})

	It("writes a Docker archive when the configured container tool is Podman", func() {
		Expect(e2eimage.SaveArgs(
			"podman", "linux/arm64", "/tmp/image.tar", "example/image:test",
		)).To(Equal([]string{
			"image", "save", "--format", "docker-archive", "--platform", "linux/arm64",
			"--output", "/tmp/image.tar", "example/image:test",
		}))
	})
})
