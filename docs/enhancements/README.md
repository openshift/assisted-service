# Enhancements Tracking for Assisted Installer

Inspired by the [Kubernetes enhancement](https://github.com/kubernetes/enhancements) process.

This directory provides a rally point to discuss, debate, and reach consensus
for how Assisted Installer (AI) enhancements are introduced. Given that the AI
is composed of multiple projects: assisted-service, assisted-installer, and
assisted-installer-agent to name a few, it is useful to have a centralized
place to describe AI enhancements via an actionable design proposal.

## Should I Create an Enhancement?

A rough heuristic for an enhancement is anything that:

- impacts multiple Assisted Installer projects
- requires significant effort to complete
- requires consensus across multiple domains of Assisted Installer
- impacts the UX or operation of Assisted Installer substantially
- users of Assisted Installer will notice and come to rely on

A rough heuristic for when an enhancement should be made in
openshift/enhancements instead:

- requires changes to OpenShift
- requires changes and/or consensus with other components related to
  OpenShift
- substanitally impacts the requirements for and/or experience of installing and
  provisioning OpenShift
- would benefit from approval by OpenShift architects

It is unlikely to require an enhancement if it:

- is covered by an existing OpenShift enhancement proposal
- fixes a bug
- adds more testing
- internally refactors a code or component only visible to that components domain
- minimal impact to Assisted Installer as a whole

## Getting Started

Follow the process outlined in the [enhancement template](template.md)
