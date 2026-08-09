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

package v1alpha2

// Hub marks Kubernaut as the conversion hub for this CRD's versions (see
// sigs.k8s.io/controller-runtime/pkg/conversion.Hub). v1alpha1 implements
// conversion.Convertible (ConvertTo/ConvertFrom against this type) in
// api/v1alpha1/kubernaut_conversion.go. Per ADR-CRD-001 Decision Axis 1,
// v1alpha2 is the hub and storage version precisely because it is the
// *target* shape every future version should converge on -- picking the
// hub is a one-way door (kubebuilder book, "Webhook for Conversion"), so
// the newest/target version is the correct choice, not v1alpha1 (the
// legacy shape being converted away from).
func (*Kubernaut) Hub() {}
