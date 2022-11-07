package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const EventsChannelSize = 200

//go:generate mockgen --build_flags=--mod=mod -package controllers -destination mock_crd_events_handler.generated_go . CRDEventsHandler
type CRDEventsHandler interface {
	NotifyClusterDeploymentUpdates(clusterDeploymentName string, clusterDeploymentNamespace string)
	NotifyInfraEnvUpdates(infraEnvName string, infraEnvNamespace string)
	NotifyAgentUpdates(agentName string, agentNamespace string)
	GetInfraEnvUpdates() chan event.GenericEvent
	GetClusterDeploymentUpdates() chan event.GenericEvent
	GetAgentUpdates() chan event.GenericEvent
}

type CRDEventsHandlerChannels struct {
	clusterDeploymentUpdates chan event.GenericEvent
	infraEnvUpdates          chan event.GenericEvent
	agentUpdates             chan event.GenericEvent
}

func NewCRDEventsHandler() CRDEventsHandler {
	return &CRDEventsHandlerChannels{
		clusterDeploymentUpdates: make(chan event.GenericEvent, EventsChannelSize),
		infraEnvUpdates:          make(chan event.GenericEvent, EventsChannelSize),
		agentUpdates:             make(chan event.GenericEvent, EventsChannelSize),
	}
}

func (h *CRDEventsHandlerChannels) NotifyUpdates(ch chan<- event.GenericEvent, name string, namespace string) {
	ch <- event.GenericEvent{
		Object: &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

func (h *CRDEventsHandlerChannels) NotifyClusterDeploymentUpdates(clusterDeploymentName string, clusterDeploymentNamespace string) {
	h.NotifyUpdates(h.clusterDeploymentUpdates, clusterDeploymentName, clusterDeploymentNamespace)
}

func (h *CRDEventsHandlerChannels) NotifyAgentUpdates(agentName string, agentNamespace string) {
	h.NotifyUpdates(h.agentUpdates, agentName, agentNamespace)
}

func (h *CRDEventsHandlerChannels) NotifyInfraEnvUpdates(infraEnvName string, infraEnvNamespace string) {
	h.NotifyUpdates(h.infraEnvUpdates, infraEnvName, infraEnvNamespace)
}

func (h *CRDEventsHandlerChannels) GetInfraEnvUpdates() chan event.GenericEvent {
	return h.infraEnvUpdates
}

func (h *CRDEventsHandlerChannels) GetClusterDeploymentUpdates() chan event.GenericEvent {
	return h.clusterDeploymentUpdates
}

func (h *CRDEventsHandlerChannels) GetAgentUpdates() chan event.GenericEvent {
	return h.agentUpdates
}
