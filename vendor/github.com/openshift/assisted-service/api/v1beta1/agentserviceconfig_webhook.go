/*
Copyright 2020.

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

package v1beta1

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *AgentServiceConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/validate-agent-install-openshift-io-v1beta1-agentserviceconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=agent-install.openshift.io,resources=agentserviceconfigs,verbs=create;update,versions=v1beta1,name=vagentserviceconfig.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &AgentServiceConfig{}

func (r *AgentServiceConfig) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (r *AgentServiceConfig) ValidateUpdate(ctx context.Context, old runtime.Object, new runtime.Object) (admission.Warnings, error) {
	var oldASC, newASC *AgentServiceConfig
	var ok bool
	if oldASC, ok = old.(*AgentServiceConfig); !ok {
		// should never happen
		return nil, nil
	}
	if newASC, ok = new.(*AgentServiceConfig); !ok {
		// should never happen
		return nil, nil
	}
	for _, annotation := range []string{SecretsPrefixAnnotation, PVCPrefixAnnotation} {
		oldValue, oldOK := oldASC.Annotations[annotation]
		newVlue, newOK := newASC.Annotations[annotation]
		if oldOK && newOK && oldValue == newVlue {
			// No change in secrets prefix, no validation needed
			return nil, nil
		}
		return nil, fmt.Errorf("can not change %s annotation from %s to %s", annotation, oldValue, newVlue)
	}
	return nil, nil
}

func (r *AgentServiceConfig) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
