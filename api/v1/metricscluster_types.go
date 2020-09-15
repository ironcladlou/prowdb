package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricsClusterSpec defines the desired state of MetricsCluster
type MetricsClusterSpec struct {
	URLs []string `json:"urls,omitempty"`
}

// MetricsClusterStatus defines the observed state of MetricsCluster
type MetricsClusterStatus struct {
}

// +kubebuilder:object:root=true

// MetricsCluster is the Schema for the metricsclusters API
type MetricsCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MetricsClusterSpec   `json:"spec,omitempty"`
	Status MetricsClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MetricsClusterList contains a list of MetricsCluster
type MetricsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MetricsCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MetricsCluster{}, &MetricsClusterList{})
}
