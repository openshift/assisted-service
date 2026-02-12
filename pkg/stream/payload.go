package stream

import "github.com/openshift/assisted-service/models"

// ClusterPayload represents cluster data sent in Kafka notification events.
// It includes fields from models.Cluster plus additional fields for
// observability that are not exposed in the REST API.
type ClusterPayload struct {
	models.Cluster
	PrimaryIPStack int `json:"primary_ip_stack,omitempty"`
}
