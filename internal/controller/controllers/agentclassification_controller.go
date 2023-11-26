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
	"fmt"
	"strings"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	AgentClassificationFinalizer = "agentclassification." + aiv1beta1.Group
	ClassificationLabelPrefix    = "agentclassification." + aiv1beta1.Group + "/"
)

// AgentClassificationReconciler reconciles a AgentClassification object
type AgentClassificationReconciler struct {
	client.Client
	Log logrus.FieldLogger
}

//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentclassifications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentclassifications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agentclassifications/finalizers,verbs=update
//+kubebuilder:rbac:groups=agent-install.openshift.io,resources=agents,verbs=get;list;watch

func (r *AgentClassificationReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := r.Log.WithFields(
		logrus.Fields{
			"agent_classification":           req.Name,
			"agent_classification_namespace": req.Namespace,
		})

	defer func() {
		log.Info("AgentClassification Reconcile ended")
	}()

	log.Info("AgentClassification Reconcile started")

	classification := &aiv1beta1.AgentClassification{}
	if err := r.Get(ctx, req.NamespacedName, classification); err != nil {
		log.WithError(err).Errorf("Failed to get AgentClassification %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Add a finalizer to newly created objects.
	if classification.DeletionTimestamp.IsZero() && !funk.ContainsString(classification.GetFinalizers(), AgentClassificationFinalizer) {
		controllerutil.AddFinalizer(classification, AgentClassificationFinalizer)
		if err := r.Update(ctx, classification); err != nil {
			log.WithError(err).Errorf("failed to add finalizer %s to resource %s %s", AgentClassificationFinalizer, classification.Name, classification.Namespace)
			return ctrl.Result{}, err
		}
	}

	matchedCount := 0
	errorCount := 0

	agents := aiv1beta1.AgentList{}
	opts := &client.ListOptions{
		Namespace: classification.Namespace,
	}
	if err := r.List(ctx, &agents, opts); err != nil {
		return ctrl.Result{}, err
	}
	matchedCount, errorCount = countAgentsByClassification(log, &agents, classification)

	setErrorCountCondition(classification, errorCount)
	classification.Status.MatchedCount = matchedCount
	classification.Status.ErrorCount = errorCount

	if !classification.DeletionTimestamp.IsZero() {
		if matchedCount > 0 {
			log.Info("waiting to delete")
		} else {
			controllerutil.RemoveFinalizer(classification, AgentClassificationFinalizer)
			if err := r.Update(ctx, classification); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from resource %s %s", AgentClassificationFinalizer, classification.Name, classification.Namespace)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	if err := r.Status().Update(ctx, classification); err != nil {
		log.WithError(err).Error("failed to update classification status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func countAgentsByClassification(log logrus.FieldLogger, agents *aiv1beta1.AgentList, classification *aiv1beta1.AgentClassification) (matchedCount, errorCount int) {
	for _, agent := range agents.Items {
		labels := agent.GetLabels()
		if labels == nil {
			continue
		}
		if _, ok := labels[ClassificationLabelPrefix+classification.Spec.LabelKey]; !ok {
			continue
		}
		if labels[ClassificationLabelPrefix+classification.Spec.LabelKey] == classification.Spec.LabelValue {
			matchedCount++
		} else if strings.HasPrefix(labels[ClassificationLabelPrefix+classification.Spec.LabelKey], "QUERYERROR") {
			errorCount++
		}
	}

	return
}

func setErrorCountCondition(classification *aiv1beta1.AgentClassification, errorCount int) {
	if errorCount != 0 {
		conditionsv1.SetStatusConditionNoHeartbeat(&classification.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.QueryErrorsCondition,
			Status:  corev1.ConditionTrue,
			Reason:  aiv1beta1.QueryHasErrorsReason,
			Message: fmt.Sprintf("%d Agents failed to apply the classification", errorCount),
		})
	} else {
		conditionsv1.SetStatusConditionNoHeartbeat(&classification.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.QueryErrorsCondition,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.QueryNoErrorsReason,
			Message: "No Agents failed to apply the classification",
		})
	}
}

func (r *AgentClassificationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapAgentToAgentClassification := func(ctx context.Context, agent client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"agent":           agent.GetName(),
				"agent_namespace": agent.GetNamespace(),
			})
		acList := &aiv1beta1.AgentClassificationList{}
		opts := &client.ListOptions{
			Namespace: agent.GetNamespace(),
		}
		if err := r.List(context.Background(), acList, opts); err != nil {
			log.Debugf("failed to list agent classifications")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(acList.Items))
		for _, classification := range acList.Items {
			reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: classification.Namespace,
				Name:      classification.Name,
			}})
		}
		return reply
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.AgentClassification{}).
		Watches(&aiv1beta1.Agent{}, handler.EnqueueRequestsFromMapFunc(mapAgentToAgentClassification)).
		Complete(r)
}
