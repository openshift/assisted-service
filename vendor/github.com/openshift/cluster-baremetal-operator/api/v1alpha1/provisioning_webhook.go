/*

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

package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var provisioninglog = logf.Log.WithName("provisioning-resource")
var enabledFeatures EnabledFeatures

func (r *Provisioning) SetupWebhookWithManager(mgr ctrl.Manager, features EnabledFeatures) error {
	enabledFeatures = features
	return ctrl.NewWebhookManagedBy(mgr, r).
		WithValidator(r).
		Complete()
}

// https://golangbyexample.com/go-check-if-type-implements-interface/
var _ admission.Validator[*Provisioning] = &Provisioning{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (r *Provisioning) ValidateCreate(ctx context.Context, obj *Provisioning) (admission.Warnings, error) {
	provisioninglog.Info("validate create", "name", obj.Name)

	if obj.Name != ProvisioningSingletonName {
		return nil, fmt.Errorf("Provisioning object is a singleton and must be named \"%s\"", ProvisioningSingletonName)
	}

	return nil, obj.ValidateBaremetalProvisioningConfig(enabledFeatures)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (r *Provisioning) ValidateUpdate(ctx context.Context, oldObj, newObj *Provisioning) (admission.Warnings, error) {
	provisioninglog.Info("validate update", "name", newObj.Name)
	return nil, newObj.ValidateBaremetalProvisioningConfig(enabledFeatures)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (r *Provisioning) ValidateDelete(ctx context.Context, obj *Provisioning) (admission.Warnings, error) {
	provisioninglog.Info("validate delete", "name", obj.Name)
	return nil, nil
}
