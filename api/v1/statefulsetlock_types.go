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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StatefulSetLockSpec defines the desired state of StatefulSetLock
type StatefulSetLockSpec struct {
	// StatefulSetName is the name of the target StatefulSet in the same namespace
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	StatefulSetName string `json:"statefulSetName"`

	// LeaseName is the name of the coordination.k8s.io/v1.Lease object
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	LeaseName string `json:"leaseName"`

	// LeaseDurationSeconds is the duration in seconds that the lease is valid
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds"`
}

// StatefulSetLockStatus defines the observed state of StatefulSetLock
type StatefulSetLockStatus struct {
	// WriterPod is the name of the pod currently holding the writer lock
	WriterPod string `json:"writerPod,omitempty"`

	// ObservedGeneration is the last generation reconciled by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the StatefulSetLock
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ssl
// +kubebuilder:printcolumn:name="StatefulSet",type="string",JSONPath=".spec.statefulSetName"
// +kubebuilder:printcolumn:name="Writer Pod",type="string",JSONPath=".status.writerPod"
// +kubebuilder:printcolumn:name="Lease Duration",type="integer",JSONPath=".spec.leaseDurationSeconds"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// StatefulSetLock is the Schema for the statefulsetlocks API
type StatefulSetLock struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StatefulSetLockSpec   `json:"spec,omitempty"`
	Status StatefulSetLockStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StatefulSetLockList contains a list of StatefulSetLock
type StatefulSetLockList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StatefulSetLock `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StatefulSetLock{}, &StatefulSetLockList{})
}
