---
title: automatic-agent-classification-labels
authors:
  - "@avishay"
creation-date: 2022-02-27
last-updated: 2022-02-27
---

# Automatic Agent Classification Labels

## Summary

When using late binding, users select which Agents should be part of a cluster based on their inventory. The selection can be manual or by using a label selector.
Two kinds of labels are helpful:

- Inventory labels: Simply represents the hardware, such as architecture, virtualization support, etc. The agent controller already adds these labels.
- User-defined labels: These are labels that a user would add to characterize their hardware. Examples include “t-shirt sizes” or hosts suitable for hosting storage or machine learning workloads. There is currently no automation for adding these labels.

This proposal is to automate user-defined labels. Users would define the desired labels and conditions, and the system would automate the process.

## Motivation

Having users define labels manually is time-consuming and error-prone. Defining conditions is more reliable and scalable for users.

### Goals

- Provide a flexible and easy-to-use API for defining desired labels and conditions.
- Extend the Infrastructure Operator to maintain the desired labels based on the specified conditions with minimal impact on scale.

### Non-Goals

- Ship pre-defined labels and conditions.

## Proposal

### User Stories

#### Story 1

As a Cluster Creator, I want the system to automatically label Agents based on specific conditions so that I can more easily select Agents for cluster membership.

## Design Details [optional]

We will create a new AgentClassification namespace-scoped CRD that defines the desired label key-value and a query to be evaluated according to the Agent’s inventory.

### AgentClassification CRD Definition

```
const (
	QueryValidCondition conditionsv1.ConditionType = "QueryValid"
	QueryValidReason    string                     = "QueryIsValid"
	QueryNotValidReason string                     = "QueryIsNotValid"

	QueryErrorsCondition conditionsv1.ConditionType = "QueryErrors"
	QueryErrorsOK        string                     = "NoQueryErrors"
	QueryHasErrors       string                     = "HasQueryErrors"
)

// AgentClassificationSpec defines the desired state of AgentClassification
type AgentClassificationSpec struct {
	// LabelKey specifies the label key to apply to matched Agents
	//
	// +immutable
	LabelKey string `json:"labelKey"`

	// LabelValue specifies the label value to apply to matched Agents
	//
	// +immutable
	LabelValue string `json:"labelValue"`

	// Query is in gojq format (https://github.com/itchyny/gojq#difference-to-jq)
	// and will be invoked on each Agent's inventory. The query should return a
	// boolean. The operator will apply the label to any Agent for which "true"
	// is returned.
	Query string `json:"query"`
}

// AgentClassificationStatus defines the observed state of AgentClassification
type AgentClassificationStatus struct {
	// MatchedCount shows how many Agents currently match the classification
	MatchedCount int `json:"matchedCount,omitempty"`

	// ErrorCount shows how many Agents encountered errors when matching the classification
	ErrorCount int `json:"errorCount,omitempty"`

	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`
}
```

We will base the expression definition on the [gojq](https://github.com/itchyny/gojq) library, which supports jq queries in Go. The query will be run on each Agent's inventory. If the query returns true, then the specified label will be applied.

For example,
The query for label "size:medium" might be: ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592"
The query for label "storage:large" might be: "[.disks[] | select(.sizeBytes > 1073741824000)] | length > 5"

### Operator changes

A new agent-label-controller will reconcile Agent CRs and update labels on Agents as necessary. It will be triggered by updates to both Agents and AgentClassifications via watch mapping. The controller lists the AgentClassifications in the Agent's namespace and for each:

- If the AgentClassifications is being deleted or if the query returns false, deletes the label from the Agent if it exists
- If the query returns true, set the label if not already set (the label key will be prefixed with inventoryclassification.agent-install.openshift.io/)
- If the query fails to run, set the label key, but the value to "QUERYERROR-\<value\>"
- An annotation inventoryclassification.agent-install.openshift.io/updatedat will be set with the current timestamp to help debugging

A new agent-classification-controller will reconcile AgentClassification CRs. It will be triggered by updates to both AgentClassifications and Agents via watch mapping. The controller first:

- Tries to compile the query, and if it fails, sets QueryValidCondition to false with a proper message.
- Sets a finalizer to ensure the AgentClassification is deleted only after no Agents have its label set.
  The controller then list the Agents in the AgentClassification's namespace and counts how many Agents have the label key/value (Status.MatchedCount) or how many have the key with the value set to "QUERYERROR" (Status.ErrorCount).
  If Status.ErrorCount is not zero, then QueryErrorsCondition is set.
  If the AgentClassification is being deleted and Status.MatchedCount and Status.ErrorCount are both zero, then it is deleted - otherwise it will check again in the next reconcile.

### Risks and Mitigations

With a large number of Agents and AgentClassifications, processing these labels can take a long time. To limit this, we scope the effects of an AgentClassifications to its namespace. To avoid having this impact installations, we move the processing to separate controllers.

### Open Questions

### UI Impact

None.

### Test Plan

Unit and subsystem test.

## Drawbacks

## Alternatives
