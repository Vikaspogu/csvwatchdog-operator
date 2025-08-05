/*
Copyright 2025.

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

// CSVWatchdogSpec defines the desired state of CSVWatchdog.
type CSVWatchdogSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of CSVWatchdog. Edit csvwatchdog_types.go to remove/update
	GitHubCredentials GitHubCredentials `json:"gitHubCredentials,omitempty"`
	RepositoryOwner   string            `json:"repositoryOwner"`
	OwnerType         string            `json:"ownerType"`
	RepositoryName    string            `json:"repositoryName"`
	Path              string            `json:"path"`
}

type GitHubCredentials struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
}

// CSVWatchdogStatus defines the observed state of CSVWatchdog.
type CSVWatchdogStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// CSVWatchdog is the Schema for the csvwatchdogs API.
type CSVWatchdog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CSVWatchdogSpec   `json:"spec,omitempty"`
	Status CSVWatchdogStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CSVWatchdogList contains a list of CSVWatchdog.
type CSVWatchdogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CSVWatchdog `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CSVWatchdog{}, &CSVWatchdogList{})
}
