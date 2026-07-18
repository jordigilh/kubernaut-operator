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

// Package v1alpha1 contains API Schema definitions for the v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=kubernaut.ai
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "kubernaut.ai", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	// Implemented directly against k8s.io/apimachinery rather than the
	// deprecated sigs.k8s.io/controller-runtime/pkg/scheme.Builder, per that
	// package's own migration guidance -- api packages should depend on
	// only the standard library, k8s.io/apimachinery, and other api
	// packages. Behavior (AddKnownTypes + AddToGroupVersion per Register
	// call) is unchanged.
	SchemeBuilder = &schemeBuilder{}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

type schemeBuilder struct {
	runtime.SchemeBuilder
}

// Register adds one or more objects to the SchemeBuilder so they can be
// added to a Scheme, mirroring the deprecated controller-runtime
// scheme.Builder.Register behavior.
func (b *schemeBuilder) Register(objects ...runtime.Object) *schemeBuilder {
	b.SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, objects...)
		metav1.AddToGroupVersion(s, GroupVersion)
		return nil
	})
	return b
}
