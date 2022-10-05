# Per-customer feature enablement

## Rationale

Sometimes we implement a feature that should not be available to all the customers in SaaS, but only to selected group of accounts or organizations. In order to do so, we leverage the OCM capabilities and the fact that Assisted Service can query for those with the context of the user calling the service. This allows for a very easy implementation of the following logic

* if the Service runs on-prem, feature is always allowed
* if the Service runs in the cloud with OCM non-available, feature is always allowed
* if the Service runs in the cloud with OCM available, feature availability depends on the OCM

## Capability definition in OCM

At first OCM needs to know the notion of the capability we are introducing. In order to do so, we open a merge request in the [service-delivery/uhc-account-manager repository](https://gitlab.cee.redhat.com/service/uhc-account-manager) that modifies the `pkg/api/capability_types.go` file by adding e.g.

```golang
const CapabilityBareMetalInstallerMultiarch = "capability.account.bare_metal_installer_multiarch"
```

You can look at merge requests affecting this file to have an exhaustive example of the change.

## Querying capability in Assisted Service

In the `authzHandler` we have implemented the `HasOrgBasedCapability` function which can return the status of desired capability in the context of the user calling for the feature. A complete example of a validator leveraging this call is shown below:

```golang
func (h *handler) ValidateAccessToMultiarch(ctx context.Context, authzHandler auth.Authorizer) error {
	var err error
	var multiarchAllowed bool

	multiarchAllowed, err = authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error getting user %s capability, error: %w", ocm.MultiarchCapabilityName, err))
	}
	if !multiarchAllowed {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s", "multiarch clusters are not available"))
	}
	return nil
}
```

### Authz internals

Please note the function `HasOrgBasedCapability` internally handles a scenario when OCM is not available. Because of that, it can be safely used in all the environments with the assumption that if the OCM is not available, the returned value will be `true`. Therefore unavailability of the OCM can never cause the feature to become unaccessible. Is is not the same as malfunctioning OCM, in which case the error may be raised.

The safety of the call in non-SaaS deployments comes from the way the function is defined in the `NoneHandler` shown below:

```golang
func (*NoneHandler) HasOrgBasedCapability(ctx context.Context, capability string) (bool, error) {
	return true, nil
}
```

as opposed to the Red Hat SSO `AuthzHandler` below:

```golang
func (a *AuthzHandler) HasOrgBasedCapability(ctx context.Context, capability string) (bool, error) {
	if !a.isOrgBasedFunctionalityEnabled() {
		return true, nil
	}

	username := ocm.UserNameFromContext(ctx)
	isAllowed, err := a.client.Authorization.CapabilityReview(context.Background(), fmt.Sprint(username), capability, ocm.OrganizationCapabilityType)
	a.log.Debugf("queried AMS API with CapabilityReview for username: %s about capability: %s, capability type: %s. Result: %t",
		fmt.Sprint(username), capability, ocm.OrganizationCapabilityType, isAllowed)
	return isAllowed, err
}
```

## Example implementation

Multi-arch support in Assisted Service is one feature that uses the aforementioned functionality. The most interesting part explaining how to use the mentioned framework is shown below. The code will trigger `continue` if called by the user without proper permissions or return `err` in case of unexpected issues. If executed in the scope of no-OCM environment or if the user has all required permissions, the code below will not change the flow of the function where it's embedded.

```golang
if !checkedForMultiarchAuthorization {
  checkedForMultiarchAuthorization = true
  if err := h.ValidateAccessToMultiarch(ctx, h.authzHandler); err != nil {
    if strings.Contains(err.Error(), "multiarch clusters are not available") {
      continue
    } else {
      return common.GenerateErrorResponder(err)
    }
  }
  hasMultiarchAuthorization = true
}
if !hasMultiarchAuthorization {
  continue
}
```

The full PR containing this example and implementing all the features mentioned above is [Allow org-based filtering for multiarch images](https://github.com/openshift/assisted-service/pull/4368).
