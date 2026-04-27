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
	configv1 "github.com/openshift/api/config/v1"
)

// Named TLS profile strings consumed by kubernaut services (ADR-TLS-001).
const (
	TLSProfileNameOld          = "Old"
	TLSProfileNameIntermediate = "Intermediate"
	TLSProfileNameModern       = "Modern"
)

// MapTLSProfile converts an OpenShift TLSSecurityProfile (from the cluster
// APIServer CR) into the profile name string expected by kubernaut services.
// Returns "" when profile is nil (non-OCP or unset), which tells services to
// use Go's default TLS 1.2 settings.
//
// Custom profiles are resolved to the nearest named profile based on
// minTLSVersion, since kubernaut services only accept named profiles
// (Old/Intermediate/Modern) per ADR-TLS-001.
func MapTLSProfile(profile *configv1.TLSSecurityProfile) string {
	if profile == nil {
		return ""
	}
	switch profile.Type {
	case configv1.TLSProfileOldType:
		return TLSProfileNameOld
	case configv1.TLSProfileIntermediateType:
		return TLSProfileNameIntermediate
	case configv1.TLSProfileModernType:
		return TLSProfileNameModern
	case configv1.TLSProfileCustomType:
		return mapCustomProfile(profile.Custom)
	default:
		return ""
	}
}

func mapCustomProfile(custom *configv1.CustomTLSProfile) string {
	if custom == nil {
		return TLSProfileNameIntermediate
	}
	switch custom.MinTLSVersion {
	case configv1.VersionTLS13:
		return TLSProfileNameModern
	case configv1.VersionTLS12:
		return TLSProfileNameIntermediate
	default:
		return TLSProfileNameOld
	}
}
