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
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	qualificationEnabledEnvironment = "KUBERNAUT_E2E_QUALIFICATION"
	qualificationKeycloakImageEnv   = "KUBERNAUT_E2E_QUALIFICATION_KEYCLOAK_IMAGE"
	qualificationOperatorImageEnv   = "KUBERNAUT_E2E_QUALIFICATION_OPERATOR_IMAGE"
	qualificationContainerToolEnv   = "KUBERNAUT_E2E_CONTAINER_TOOL"

	qualificationKeycloakHostPort = 8443
	qualificationConsoleHostPort  = 8843
	qualificationKeycloakNodePort = 30557

	qualificationKeycloakNamespace = "idp"
	qualificationKeycloakService   = "keycloak"
	qualificationConsoleHost       = "kubernaut-console.local"

	qualificationKeycloakIssuer = "https://keycloak:8443/realms/kubernaut-demo"
	qualificationConsoleClient  = "kubernaut-console"
	qualificationConsoleSecret  = "e2e-console-secret" //nolint:gosec // qualification-only Keycloak client secret
	qualificationConsoleCookie  = "01234567890123456789012345678901"
	qualificationEnabledTrue    = "true"
)

var (
	immutableImageDigestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	immutableImageRepositoryPattern = regexp.MustCompile(
		`^[a-z0-9]+(?:[a-z0-9._-]*[a-z0-9])?(?::[0-9]+)?` +
			`(?:/[a-z0-9]+(?:[a-z0-9._-]*[a-z0-9])?)*` +
			`(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?$`,
	)
)

// qualificationLane is set once by BeforeSuite. The default Kind suite does
// not load application images, expose NodePorts, or create an IdP.
var qualificationLane bool

// qualificationEnabledFromEnvironment reads the explicit opt-in switch used
// by the Kind qualification lane.
func qualificationEnabledFromEnvironment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(qualificationEnabledEnvironment))) {
	case "1", qualificationEnabledTrue, "yes", "on":
		return true
	default:
		return false
	}
}

// QualificationEnabledForValue is the pure environment-selector contract
// used by the small Kind harness contract suite.
func QualificationEnabledForValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", qualificationEnabledTrue, "yes", "on":
		return true
	default:
		return false
	}
}

// ValidateImmutableImageReference rejects mutable image inputs. A tag may be
// retained before the @ for registry readability, but the digest is mandatory
// and must be a complete sha256 digest.
func ValidateImmutableImageReference(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image reference is empty")
	}
	at := strings.LastIndex(image, "@")
	if at <= 0 || at == len(image)-1 {
		return fmt.Errorf("image reference %q must include an immutable @sha256 digest", image)
	}
	if !immutableImageRepositoryPattern.MatchString(image[:at]) {
		return fmt.Errorf("image reference %q has an invalid repository", image)
	}
	if !immutableImageDigestPattern.MatchString(image[at+1:]) {
		return fmt.Errorf("image reference %q has an invalid immutable digest", image)
	}
	return nil
}

// QualificationImageReferences returns every external image used by the
// real-component lane. The operator and Keycloak references are supplied by
// the caller because this repository does not approve a mutable default for
// either runtime input.
func QualificationImageReferences(keycloakImage, operatorImage string) map[string]string {
	images := qualificationOperandImages()
	images[qualificationKeycloakService] = keycloakImage
	images["operator"] = operatorImage
	return images
}

// ValidateQualificationImageReferences applies the immutable-input gate to a
// complete qualification image set.
func ValidateQualificationImageReferences(images map[string]string) error {
	if len(images) == 0 {
		return fmt.Errorf("qualification image set is empty")
	}
	for name, image := range images {
		if err := ValidateImmutableImageReference(image); err != nil {
			return fmt.Errorf("qualification image %q: %w", name, err)
		}
	}
	return nil
}

// qualificationKeycloakImage returns the administrator-supplied immutable
// Keycloak reference for a qualification run.
func qualificationKeycloakImage() string {
	return strings.TrimSpace(os.Getenv(qualificationKeycloakImageEnv))
}

// qualificationOperatorImage returns the qualification-specific operator
// reference, falling back to the standard operator override.
func qualificationOperatorImage() string {
	if image := strings.TrimSpace(os.Getenv(qualificationOperatorImageEnv)); image != "" {
		return image
	}
	return strings.TrimSpace(os.Getenv("KUBERNAUT_OPERATOR_IMAGE"))
}

// qualificationContainerTool selects the local image-store command used to
// transfer qualification images into Kind.
func qualificationContainerTool() string {
	if tool := strings.TrimSpace(os.Getenv(qualificationContainerToolEnv)); tool != "" {
		return tool
	}
	return "docker"
}

// QualificationImageLoadUsesArchive reports whether the selected container
// tool needs an archive handoff before an image can be loaded into Kind.
func QualificationImageLoadUsesArchive(containerTool string) bool {
	return strings.EqualFold(strings.TrimSpace(containerTool), "podman")
}

// validateQualificationEnvironment rejects missing or mutable qualification
// inputs before the disposable Kind cluster is created.
func validateQualificationEnvironment() error {
	keycloakImage := qualificationKeycloakImage()
	operatorImage := qualificationOperatorImage()
	if keycloakImage == "" {
		return fmt.Errorf("%s must be set to an immutable Keycloak image", qualificationKeycloakImageEnv)
	}
	if operatorImage == "" {
		return fmt.Errorf(
			"%s or KUBERNAUT_OPERATOR_IMAGE must be set to an immutable operator image",
			qualificationOperatorImageEnv,
		)
	}
	images := QualificationImageReferences(keycloakImage, operatorImage)
	if err := ValidateQualificationImageReferences(images); err != nil {
		return err
	}
	return nil
}

// qualificationOperandImages returns the pinned application images used by
// the real-component qualification journey.
func qualificationOperandImages() map[string]string {
	return map[string]string{
		"gateway": "quay.io/kubernaut-ai/gateway@" +
			"sha256:163b73718e2b5d95d5a5505baeb35b431e28408d2b634ed47fead9e24acfea58",
		"datastorage": "quay.io/kubernaut-ai/datastorage@" +
			"sha256:4b32f3201f2f876ee7636003592cd6bf216f480634576c5f873a44bffe727084",
		"aianalysis": "quay.io/kubernaut-ai/aianalysis@" +
			"sha256:36330cfc86e3aa5c5525066a921b655e5a5109b04efa2e852dd1e368f994a04d",
		"signalprocessing": "quay.io/kubernaut-ai/signalprocessing@" +
			"sha256:67043980d66bd782aadf76bd2c4344995197fda67f92ed62474cf3780633770c",
		"remediationorchestrator": "quay.io/kubernaut-ai/remediationorchestrator@" +
			"sha256:86cde42501683be6c61f7fc577a00fdbc6f26e849f795c03ffee69032d846cd7",
		"workflowexecution": "quay.io/kubernaut-ai/workflowexecution@" +
			"sha256:7865bd09537a497d0608077d9c6fb380c64ca41c67e90d7d7c02abdb38b06800",
		"effectivenessmonitor": "quay.io/kubernaut-ai/effectivenessmonitor@" +
			"sha256:d4b57690d596f64b9e6a85ee0e37206e93186c2fccb410e93dcac5088511e332",
		"notification": "quay.io/kubernaut-ai/notification@" +
			"sha256:34b04d8e441fef4ab8246b9227abd15f336774814ffc88564eef496d848a02f7",
		"kubernautagent": "quay.io/kubernaut-ai/kubernautagent@" +
			"sha256:dee64e07d21ec82eaf10a9569a13aea023d8511b19e6ae512306ec22bec40f72",
		"authwebhook": "quay.io/kubernaut-ai/authwebhook@" +
			"sha256:4f81055a9b8ba9003e9c8ec1739ee5e3cf523e68b3b8d66b178e12eca75fae7f",
		"apifrontend": "quay.io/kubernaut-ai/apifrontend@" +
			"sha256:9993d71cfc7eff94f2e19ad40d845cbb3cfe7579b5d680d344303daed1e98d36",
		"fleetmetadatacache": "quay.io/kubernaut-ai/fleetmetadatacache@" +
			"sha256:a59fa06ea41692fbd76f01c84bbe77ab767620151f2daef8a55594a00177ecaa",
		"db-migrate": "quay.io/kubernaut-ai/db-migrate@" +
			"sha256:dbecf98c652b0cd9f98214179cfeba3d8488f34d6b73b76a18340b6c4291fc3d",
		"console": "quay.io/kubernaut-ai/kubernaut-console@" +
			"sha256:85cdbb3596ba96873e5b30b1c861b6b96900427b61bd6001640c1789ad6c2edb",
		"oauth2-proxy": "quay.io/oauth2-proxy/oauth2-proxy@" +
			"sha256:8498b0d0ef0a7b29686414000a08aee467f02d0299c9ed1e006a8f33fc017916",
		"init-ubi-minimal": "registry.access.redhat.com/ubi10/ubi-minimal@" +
			"sha256:7dc60d7777e010c50f5e041ff069112b379c3d5eef2823d20871c67cf663f10c",
	}
}

// qualificationImageOverrides returns pinned image overrides excluding the
// gateway image, which is selected by the existing Kind journey.
func qualificationImageOverrides() map[string]string {
	images := qualificationOperandImages()
	delete(images, "gateway")
	return images
}
