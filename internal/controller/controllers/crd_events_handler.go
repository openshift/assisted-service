package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

//go:generate mockgen -package controllers -destination mock_crd_events_handler.go . CRDEventsHandler
type CRDEventsHandler interface {
	NotifyClusterDeploymentUpdates(clusterDeploymentName string, clusterDeploymentNamespace string)
	NotifyInstallEnvUpdates(installEnvName string, installEnvNamespace string)
	NotifyAgentUpdates(agentName string, agentNamespace string)
	GetInstallEnvUpdates() chan event.GenericEvent
	GetClusterDeploymentUpdates() chan event.GenericEvent
	GetAgentUpdates() chan event.GenericEvent
}

type CRDEventsHandlerChannels struct {
	clusterDeploymentUpdates chan event.GenericEvent
	installEnvUpdates        chan event.GenericEvent
	agentUpdates             chan event.GenericEvent
}

func NewCRDEventsHandler() CRDEventsHandler {
	return &CRDEventsHandlerChannels{
		clusterDeploymentUpdates: make(chan event.GenericEvent),
		installEnvUpdates:        make(chan event.GenericEvent),
		agentUpdates:             make(chan event.GenericEvent),
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
	h.NotifyUpdates(h.clusterDeploymentUpdates, clusterDeploymentName, clusterDeploymentNamespace)
}

func (h *CRDEventsHandlerChannels) NotifyAgentUpdates(agentName string, agentNamespace string) {
	h.NotifyUpdates(h.agentUpdates, agentName, agentNamespace)
}

func (h *CRDEventsHandlerChannels) NotifyInstallEnvUpdates(installEnvName string, installEnvNamespace string) {
	h.NotifyUpdates(h.installEnvUpdates, installEnvName, installEnvNamespace)
}

func (h *CRDEventsHandlerChannels) GetInstallEnvUpdates() chan event.GenericEvent {
	return h.installEnvUpdates
}

func (h *CRDEventsHandlerChannels) GetClusterDeploymentUpdates() chan event.GenericEvent {
	return h.clusterDeploymentUpdates
}

func (h *CRDEventsHandlerChannels) GetAgentUpdates() chan event.GenericEvent {
	return h.agentUpdates
}
