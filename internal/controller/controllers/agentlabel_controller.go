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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type AgentLabelReconciler struct {
	client.Client
	Log logrus.FieldLogger
}

//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents/status,verbs=get
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentclassifications,verbs=get;list;watch;create;update;patch;delete

func (r *AgentLabelReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := r.Log.WithFields(
		logrus.Fields{
			"agent_label":           req.Name,
			"agent_label_namespace": req.Namespace,
		})

	defer func() {
		log.Info("AgentLabel Reconcile ended")
	}()

	log.Info("AgentLabel Reconcile started")

	agent := &aiv1beta1.Agent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		log.WithError(err).Errorf("Failed to get AgentClassification %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get the inventory into interfaces by way of json marshal/unmarshal
	var inventoryInterface interface{}
	jsonInventory, _ := json.Marshal(agent.Status.Inventory)
	_ = json.Unmarshal(jsonInventory, &inventoryInterface)

	classifications := aiv1beta1.AgentClassificationList{}
	opts := &client.ListOptions{
		Namespace: agent.Namespace,
	}
	err := r.List(ctx, &classifications, opts)
	if err != nil {
		return ctrl.Result{}, err
	}

	changed := false
	for _, classification := range classifications.Items {
		if !classification.DeletionTimestamp.IsZero() {
			log.Infof("classification %s is being deleted", classification.Name)
			changed = deleteAgentLabel(log, agent, ClassificationLabelPrefix+classification.Spec.LabelKey, classification.Spec.LabelValue) || changed
			continue
		}

		query, err := gojq.Parse(classification.Spec.Query)
		if err != nil {
			// Should not happen - validated via webhook
			log.Errorf("Failed to parse query: %s\n", query)
			changed = setAgentLabel(log, agent, ClassificationLabelPrefix+classification.Spec.LabelKey, queryErrorValue(classification.Spec.LabelValue)) || changed
			continue
		}

		matched, err := checkMatch(log, query, inventoryInterface)
		if err != nil {
			changed = setAgentLabel(log, agent, ClassificationLabelPrefix+classification.Spec.LabelKey, queryErrorValue(classification.Spec.LabelValue)) || changed
		} else if !matched {
			changed = deleteAgentLabel(log, agent, ClassificationLabelPrefix+classification.Spec.LabelKey, classification.Spec.LabelValue) || changed
		} else {
			changed = setAgentLabel(log, agent, ClassificationLabelPrefix+classification.Spec.LabelKey, classification.Spec.LabelValue) || changed
		}
	}

	if changed {
		if err := r.Update(ctx, agent); err != nil {
			log.WithError(err).Error("failed to update agent")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func queryErrorValue(originalValue string) string {
	return fmt.Sprintf("QUERYERROR-%s", originalValue)
}

func checkMatch(log *logrus.Entry, query *gojq.Query, inventoryInterface interface{}) (bool, error) {
	iter := query.Run(inventoryInterface)
	values := []interface{}{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return false, err
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		return false, errors.New("Expected boolean, found no values")
	}
	if len(values) > 1 {
		return false, errors.New("Expected boolean, found multiple values")
	}
	value := values[0]
	if res, ok := value.(bool); ok {
		if res {
			return true, nil
		}
	}
	return false, nil
}

func deleteAgentLabel(log *logrus.Entry, agent *aiv1beta1.Agent, labelKey, labelValue string) bool {
	labels := agent.GetLabels()

	if labels == nil {
		return false
	}

	if _, ok := labels[labelKey]; !ok {
		return false
	}

	// If the label has a different value, then don't delete
	if val, ok := labels[labelKey]; ok {
		if val != labelValue && val != queryErrorValue(labelValue) {
			return false
		}
	}

	delete(labels, labelKey)
	agent.SetLabels(labels)
	log.Infof("Deleted label %s from agent %s/%s", labelKey, agent.Namespace, agent.Name)
	return true
}

func (r *AgentLabelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapAgentClassificationToAgent := func(ctx context.Context, classification client.Object) []reconcile.Request {
		log := logutil.FromContext(ctx, r.Log).WithFields(
			logrus.Fields{
				"classification":           classification.GetName(),
				"classification_namespace": classification.GetNamespace(),
			})
		agentList := &aiv1beta1.AgentList{}
		opts := &client.ListOptions{
			Namespace: classification.GetNamespace(),
		}
		if err := r.List(ctx, agentList, opts); err != nil {
			log.Debugf("failed to list agents")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(agentList.Items))
		for _, agent := range agentList.Items {
			reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: agent.Namespace,
				Name:      agent.Name,
			}})
		}
		return reply
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.Agent{}).
		Watches(&aiv1beta1.AgentClassification{}, handler.EnqueueRequestsFromMapFunc(mapAgentClassificationToAgent)).
		Complete(r)
}
