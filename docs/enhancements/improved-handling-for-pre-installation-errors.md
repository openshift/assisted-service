---
title: Improved handling for pre installation errors
authors:
  - "@pmaidmen"
creation-date: 2023-07-29
last-updated: 2023-07-29
---

# Summary

When a cluster installation is triggered, either via Assisted Installer KubeAPI or via the UI, there are preparation steps that take place before the installation is launched. Presently, not all of these failures are appropriately reported back to users and there is a lack of consistency.
The aim of this enhancement is to come up with a set of changes that improve this situation greatly.
Please see MGMT-12397 for more background on this.

# Issues identified

There are a number of issues that contribute to a lack of clarity for the end user.

* There is no guarantee that every preinstallation failure will be reported back to the user, either via KubeAPI or via the UI.
* Some parts of the preinstallation depend on other conditions, at least with these the cause of the error can be somewhat determined.
* The UI does not show enough detail in the error summary for the user to have a clue why an error occurred.
* Inconsistent error handling here https://github.com/openshift/assisted-service/blob/master/internal/bminventory/inventory.go#L1293-L1360
    - Sure, every error is handled but not enough is done to ensure this is comminicated in a useful way to the user
    - Also inconsistent terminology is used here, making it unclear if it's important that we are failing a Preparation or a Preinstallation
* If using a KubeAPI based implementation such as MCE then the installation can get into an "ininite loop" because the statemachine transitions back to "ready" when a Pre Installation error occurs. We should ideally constrain this some so that fruitless attempts can be halted.
    

# Motivation

We want to improve the general user experience in this area so that customers will quickly be able to fix common problems with less assistance.

### Goals

- In KubeAPI - Constrain installation to a maximum number of attenmpts then hold the installation to prevent fruitless retries.
- In KubeAPI - When an installation is held after a maximum number of attempts - Set a condition to explain the reason of the last failure.
- In the Cluster Object - add an additional field to store the reason for a preinstallation failure.
- In cluster validation, add a validation to show the status of cluster preinstallation and any errors.
- Ensure that the UI is updated appropriately to display failure information.

### Non-Goals

- TBC

### Implementation proposal 

    - Add a cluster "pre installation status" validation

        * This will always succeed provided that the content of the field `InstallationPreparationCompletionStatus` is set to any value other than "failure"
        * This is mainly to allow for the possibility that the check may not have been executed yet. (Similar to disk speed check.)
        * This validation should include some information on the reason for the failure if relevant.
        * We will introduce `InstallationPreparationCompletionStatusText` as a cluster db field then to populate this with the error message content 
        * `InstallationPreparationCompletionStatusText` will be set by the same function as `InstallationPreparationCompletionStatus`

### Implementation proposal - 

    - Improve the UI to present this information

        There seem to be a couple of options here...

        - UI should be improved so that when the validation 'installation-preparation-succeeded' fails, that the `message` component of the validation are included in any communication to the user, this would make it easier to determine what has gone wrong.

        or 

        - UI should be improved so that when the validation 'installation-preparation-succeeded' fails, we see a message informing us to check events for more details.
        
        I prefer the former option as the latter seems a lot less "user frindly" though understoof that this may contribute to challenges around localization and so on.

### User Stories

#### Story 1

A user who is installing a cluster using KubeAPI makes a mistake in one of their manifests, rendering it non compliant. This causes a pre installation failure every time the installation is automatically re-attempted. 

* This user should be able to specify a limit on how many times the installation will be attempted in this scenario
* Once the maximum number of attempts has been reached, a hold should be put on the installation
* While the installation is held, the user should see that the AgentClusterInstall has a condition that clearly explains why this hold was applied.
* The last error message encountered during preinstallation will be included in this condition.

#### Story 2

A user who is installing a cluster using KubeAPI makes a mistake in one of their network setup, this means that the cluster images will not resolve correctly. This causes a pre installation failure every time the installation is automatically re-attempted. 

* This user should be able to specify a limit on how many times the installation will be attempted in this scenario
* Once the maximum number of attempts has been reached, a hold should be put on the installation
* While the installation is held, the user should see that the AgentClusterInstall has a condition that clearly explains why this hold was applied.
* This erorr should advise the user to check the other conditions in the AgentClusterInstall for the root cause.

(I actually think this is not very user friendly, it might require more effort but we should consider another task to make preinstallation error reporting more consistent.)

### Story 3

A user who is installing via the API endpoints makes a mistake in one of their manifests, rendering it non compliant. This leads to a failure during preparation.

- The cause of the error should be clearly shown in the validations of the cluster so that the user is aware of what went wrong.

### Story 4

A user who is installing via the UI makes a mistake in one of their manifests, rendering it non compliant. This leads to a failure during preparation.

- When the user is returned to the "Review and create" screen they should be presented with a summary of the error, alongside the reason for the failure.

### Risks and Mitigations

- TBC

### Open Questions

    - I think we should take a look at the code in this area and consider whether or not we are doing enough to report preinstallation errors in general 
    https://github.com/openshift/assisted-service/blob/master/internal/bminventory/inventory.go#L1293-L1360

### UI Impact
The UI will need to be changed to support the error reporting requirements of this proposal.

### Test Plan

* Subsystem tests that set up various pre installation failures should suffice for testing the API responses.
* KubeAPI subsytem tests should be able to test KUBEAPI related behaviour including installation reattempts.
* Manual tests of everything
* Manual tests in the UI
* Unit tests as required.

## Drawbacks

- Provided that the solution is well tested and that we address the issues outlined here, I don't see any real drawbacks.


### Known issues
- None at present though there is an open question.