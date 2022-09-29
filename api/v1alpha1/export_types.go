/*
Copyright 2022.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ExportSpec defines the desired state of Export
type ExportSpec struct {
	ProviderType          string `json:"providerType"`
	StorageObject         string `json:"storageObject"`
	PvcSize               int    `json:"pvcSize"`
	CredSecret            string `json:"credSecret"`
	ImageSetConfiguration string `json:"imageSetConfiguration"`
	DockerConfigSecret    string `json:"dockerConfigSecret"`
}

// ExportStatus defines the observed state of Export
type ExportStatus struct {
	Synced         bool `json:"synced,omitempty"`
	Mirrored       bool `json:"mirrored,omitempty"`
	RegistryOnline bool `json:"registryOnline,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Export is the Schema for the exports API
type Export struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExportSpec   `json:"spec,omitempty"`
	Status ExportStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ExportList contains a list of Export
type ExportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Export `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Export{}, &ExportList{})
}
