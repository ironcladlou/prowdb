package api

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type MetricsCluster struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec              MetricsClusterSpec `json:"spec"`
}

type MetricsClusterSpec struct {
	URLs []string `json:"urls"`
}
