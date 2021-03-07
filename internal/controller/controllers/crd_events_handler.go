package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

//go:generate mockgen -package controllers -destination mock_crd_events_handler.go . CRDEventsHandler
type CRDEventsHandler interface {
	NotifyClusterDeploymentUpdates(clusterDeploymentName string, clusterDeploymentNamespace string)
	NotifyInstallEnvUpdates(installEnvName string, installEnvNamespace string)
	GetInstallEnvUpdates() chan event.GenericEvent
	GetClusterDeploymentUpdates() chan event.GenericEvent
}

type CRDEventsHandlerChannels struct {
	ClusterDeploymentUpdates chan event.GenericEvent
	InstallEnvUpdates        chan event.GenericEvent
}

func NewCRDEventsHandler() CRDEventsHandler {
	clusterDeploymentUpdates := make(chan event.GenericEvent)
	installEnvUpdates := make(chan event.GenericEvent)
	return &CRDEventsHandlerChannels{
		ClusterDeploymentUpdates: clusterDeploymentUpdates,
		InstallEnvUpdates:        installEnvUpdates,
	}
}

func (h *CRDEventsHandlerChannels) NotifyUpdates(ch chan<- event.GenericEvent, name string, namespace string) {
	ch <- event.GenericEvent{
		Meta: &metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func (h *CRDEventsHandlerChannels) NotifyClusterDeploymentUpdates(clusterDeploymentName string, clusterDeploymentNamespace string) {
	h.NotifyUpdates(h.ClusterDeploymentUpdates, clusterDeploymentName, clusterDeploymentNamespace)
}

func (h *CRDEventsHandlerChannels) NotifyInstallEnvUpdates(installEnvName string, installEnvNamespace string) {
	h.NotifyUpdates(h.InstallEnvUpdates, installEnvName, installEnvNamespace)
}

func (h *CRDEventsHandlerChannels) GetInstallEnvUpdates() chan event.GenericEvent {
	return h.InstallEnvUpdates
}
func (h *CRDEventsHandlerChannels) GetClusterDeploymentUpdates() chan event.GenericEvent {
	return h.ClusterDeploymentUpdates
}
