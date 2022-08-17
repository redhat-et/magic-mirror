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

// ImportSpec defines the desired state of Import
type ImportSpec struct {
	SourceType     string `json:"sourceType"`
	Source         string `json:"source"`
	PvcSize        int    `json:"pvcSize"`
	ImageConfigMap string `json:"imageConfigMap"`
}

// ImportStatus defines the observed state of Import
type ImportStatus struct {
	Synced   bool `json:"synced,omitempty"`
	Mirrored bool `json:"mirrored,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Import is the Schema for the imports API
type Import struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImportSpec   `json:"spec,omitempty"`
	Status ImportStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ImportList contains a list of Import
type ImportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Import `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Import{}, &ImportList{})
}
