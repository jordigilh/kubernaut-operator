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

// Package image provides the platform-specific image transfer used by the
// Kind-based end-to-end suites.
package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PlatformEnvironment overrides the platform exported into a Kind image
// archive. It is set explicitly by hosted Linux CI and defaults to the
// current Go runtime architecture for local runs.
const PlatformEnvironment = "KUBERNAUT_KIND_IMAGE_PLATFORM"

// Runner executes one external command and returns its combined output.
type Runner func(context.Context, string, ...string) (string, error)

// Platform returns the target Linux platform for images loaded into Kind.
func Platform() string {
	if platform := strings.TrimSpace(os.Getenv(PlatformEnvironment)); platform != "" {
		return platform
	}
	return "linux/" + runtime.GOARCH
}

// SaveArgs returns arguments for exporting an image as a Docker-compatible
// archive. Docker selects the target platform during save. Podman does not
// support --platform on image save, so its platform is selected during pull or
// retained from the already-local image without contacting the registry.
func SaveArgs(containerTool, platform, archivePath, image string) []string {
	args := []string{"image", "save"}
	if strings.EqualFold(strings.TrimSpace(containerTool), "podman") {
		return append(args, "--format", "docker-archive", "--output", archivePath, image)
	}
	return append(args, "--platform", platform, "--output", archivePath, image)
}

// LoadArgs returns arguments for importing a Docker-compatible archive into a
// named Kind cluster.
func LoadArgs(archivePath, clusterName string) []string {
	return []string{"load", "image-archive", archivePath, "--name", clusterName}
}

// Load exports one platform of an image and imports that archive into Kind.
// This avoids Kind's --all-platforms containerd import failure when the local
// image store retains an index whose non-native platform blobs are absent.
func Load(ctx context.Context, run Runner, containerTool, imageName, clusterName string) error {
	archiveDir, err := os.MkdirTemp("", "kubernaut-kind-image-")
	if err != nil {
		return fmt.Errorf("creating image archive directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(archiveDir); removeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: removing image archive directory %q failed: %v\n", archiveDir, removeErr)
		}
	}()

	archivePath := filepath.Join(archiveDir, "image.tar")
	if _, err := run(ctx, containerTool, SaveArgs(containerTool, Platform(), archivePath, imageName)...); err != nil {
		return fmt.Errorf("saving image %q for kind: %w", imageName, err)
	}
	if _, err := run(ctx, "kind", LoadArgs(archivePath, clusterName)...); err != nil {
		return fmt.Errorf("loading image archive %q into kind: %w", imageName, err)
	}
	return nil
}
