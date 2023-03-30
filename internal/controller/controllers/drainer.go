package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubectl/pkg/drain"
)

//go:generate mockgen --build_flags=--mod=mod -package=controllers -destination=mock_drainer.go . Drainer
type Drainer interface {
	RunCordonOrUncordon(helper *drain.Helper, node *corev1.Node, desired bool) error
	RunNodeDrain(helper *drain.Helper, nodeName string) error
}

type KubectlDrainer struct{}

var _ Drainer = &KubectlDrainer{}

func (d *KubectlDrainer) RunCordonOrUncordon(helper *drain.Helper, node *corev1.Node, desired bool) error {
	return drain.RunCordonOrUncordon(helper, node, desired)
}

func (d *KubectlDrainer) RunNodeDrain(helper *drain.Helper, nodeName string) error {
	return drain.RunNodeDrain(helper, nodeName)
}
