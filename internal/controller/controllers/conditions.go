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

package controllers

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

const (

	//Common
	SyncedOkReason     string = "SyncOK"
	SyncedOkMsg        string = "The Spec has been successfully applied"
	BackendErrorReason string = "BackendError"
	BackendErrorMsg    string = "The Spec could not be synced due to backend error:"
	InputErrorReason   string = "InputError"
	InputErrorMsg      string = "The Spec could not be synced due to an input error:"

	NotAvailableReason string = "NotAvailable"
	NotAvailableMsg    string = "Information not available"

	ValidationsPassingReason string = "ValidationsPassing"
	ValidationsUnknownReason string = "ValidationsUnknown"
	ValidationsFailingReason string = "ValidationsFailing"

	InstalledReason              string = "InstallationCompleted"
	InstalledMsg                 string = "The installation has completed:"
	InstallationFailedReason     string = "InstallationFailed"
	InstallationFailedMsg        string = "The installation has failed:"
	InstallationNotStartedReason string = "InstallationNotStarted"
	InstallationNotStartedMsg    string = "The installation has not yet started"
	InstallationInProgressReason string = "InstallationInProgress"
	InstallationInProgressMsg    string = "The installation is in progress:"
	UnknownStatusReason          string = "UnknownStatus"
	UnknownStatusMsg             string = "The installation status is currently not recognized:"

	// ClusterInstall Conditions
	ClusterSpecSyncedCondition string = "SpecSynced"

	ClusterCompletedCondition string = hivev1.ClusterInstallCompleted

	ClusterRequirementsMetCondition  string = hivev1.ClusterInstallRequirementsMet
	ClusterReadyReason               string = "ClusterIsReady"
	ClusterReadyMsg                  string = "The cluster is ready to begin the installation"
	ClusterNotReadyReason            string = "ClusterNotReady"
	ClusterNotReadyMsg               string = "The cluster is not ready to begin the installation"
	ClusterAlreadyInstallingReason   string = "ClusterAlreadyInstalling"
	ClusterAlreadyInstallingMsg      string = "The cluster requirements are met"
	ClusterInstallationStoppedReason string = "ClusterInstallationStopped"
	ClusterInstallationStoppedMsg    string = "The cluster installation stopped"
	ClusterInsufficientAgentsReason  string = "InsufficientAgents"
	ClusterInsufficientAgentsMsg     string = "The cluster currently requires %d agents but only %d have registered"
	ClusterUnapprovedAgentsReason    string = "UnapprovedAgents"
	ClusterUnapprovedAgentsMsg       string = "The installation is pending on the approval of %d agents"

	ClusterValidatedCondition    string = "Validated"
	ClusterValidationsOKMsg      string = "The cluster's validations are passing"
	ClusterValidationsUnknownMsg string = "The cluster's validations have not yet been calculated"
	ClusterValidationsFailingMsg string = "The cluster's validations are failing:"

	ClusterFailedCondition string = hivev1.ClusterInstallFailed
	ClusterFailedReason    string = "InstallationFailed"
	ClusterFailedMsg       string = "The installation failed:"
	ClusterNotFailedReason string = "InstallationNotFailed"
	ClusterNotFailedMsg    string = "The installation has not failed"

	ClusterStoppedCondition       string = hivev1.ClusterInstallStopped
	ClusterStoppedFailedReason    string = "InstallationFailed"
	ClusterStoppedFailedMsg       string = "The installation has stopped due to error"
	ClusterStoppedCanceledReason  string = "InstallationCancelled"
	ClusterStoppedCanceledMsg     string = "The installation has stopped because it was cancelled"
	ClusterStoppedCompletedReason string = "InstallationCompleted"
	ClusterStoppedCompletedMsg    string = "The installation has stopped because it completed successfully"
	ClusterNotStoppedReason       string = "InstallationNotStopped"
	ClusterNotStoppedMsg          string = "The installation is waiting to start or in progress"

	//Agent Conditions
	SpecSyncedCondition conditionsv1.ConditionType = "SpecSynced"

	ConnectedCondition      conditionsv1.ConditionType = "Connected"
	AgentConnectedReason    string                     = "AgentIsConnected"
	AgentDisconnectedReason string                     = "AgentIsDisconnected"
	AgentConnectedMsg       string                     = "The agent's connection to the installation service is unimpaired"
	AgentDisonnectedMsg     string                     = "The agent has not contacted the installation service in some time, user action should be taken"

	InstalledCondition conditionsv1.ConditionType = "Installed"

	ReadyForInstallationCondition  conditionsv1.ConditionType = "ReadyForInstallation"
	AgentReadyReason               string                     = "AgentIsReady"
	AgentReadyMsg                  string                     = "The agent is ready to begin the installation"
	AgentNotReadyReason            string                     = "AgentNotReady"
	AgentNotReadyMsg               string                     = "The agent is not ready to begin the installation"
	AgentAlreadyInstallingReason   string                     = "AgentAlreadyInstalling"
	AgentAlreadyInstallingMsg      string                     = "The agent cannot begin the installation because it has already started"
	AgentIsNotApprovedReason       string                     = "AgentIsNotApproved"
	AgentIsNotApprovedMsg          string                     = "The agent is not approved"
	AgentInstallationStoppedReason string                     = "AgentInstallationStopped"
	AgentInstallationStoppedMsg    string                     = "The agent installation stopped"

	ValidatedCondition         conditionsv1.ConditionType = "Validated"
	AgentValidationsPassingMsg string                     = "The agent's validations are passing"
	AgentValidationsUnknownMsg string                     = "The agent's validations have not yet been calculated"
	AgentValidationsFailingMsg string                     = "The agent's validations are failing:"
)
