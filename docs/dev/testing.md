# Assisted Installer Testing

The assisted-installer tests are divided into 3 categories:

* **Unit tests** - Focused on a module/function level while other modules are mocked.
Unit tests are located in the package, where the code they are testing resides, using the pattern `<module_name>_test.go`.
Unit tests needs a postgresql db container. The image for db container `quay.io/sclorg/postgresql-12-c8s:latest` is built from `https://github.com/sclorg/postgresql-container`

* **Subsystem tests** - Focused on the component while mocking other component.
For example, assisted-service subsystem tests mock the agent responses.

* **System tests** (a.k.a e2e) - Running full flows with all components.
The e2e tests are divided into u/s (upstream) basic workflows on [assisted-test-infra](https://github.com/openshift/assisted-test-infra/tree/master/discovery-infra/tests) and d/s (downstream) extended regression tests maintained by both DEV and QE teams on [kni-assisted-installer-auto](https://gitlab.cee.redhat.com/ocp-edge-qe/kni-assisted-installer-auto/-/tree/master/api_tests).

## Repository CI

Our CI jobs are currently managed and ran by two CI tools - a Jenkins hosted on <http://assisted-jenkins.usersys.redhat.com> and a Prow hosts on <https://prow.ci.openshift.org>.

| [Jenkins]((http://assisted-jenkins.usersys.redhat.com)) | [Prow](https://prow.ci.openshift.org) |
|---|---|
| Local for Assisted ecosystem | Company-wide |
| Checks comments for JIRA | Runs e2e
| Manages images in quay.io/edge-infrastructure | Runs all testing checks (lint, unit, etc)

Assisted-service CI jobs are defined under [openshift/release](https://github.com/openshift/release) repository on [openshift-assisted-service-master.yaml](https://github.com/openshift/release/blob/master/ci-operator/config/openshift/assisted-service/openshift-assisted-service-master.yaml).
Read more about OpenShift CI infrastructure on [OpenShift CI Docs](https://docs.ci.openshift.org/docs/).

All the currently available jobs for the openshift/assisted-service repository can be viewed on [Openshift CI Step Registry](https://steps.ci.openshift.org/search?job=openshift-assisted-service).

### Adding a new CI job

When adding a new job the following rules of thumbs should be taken into account:

* Test logic needs to be maintained in the repository under test and not under openshift/release.
It would allow easier integration with other tools, less dependency of the CI infrastructure, and most importantly the availability to run it locally.

* When introducing a new job it should be both a presubmit job and a periodic job. A presubmit job needs to be available so contributors would be able to run it on their PRs before merging.

    The presubmit job needs to be configured as `always_run: false` and `optional: true` (not blocking a merge) until proving stability.
    New OCP releases might break one of Assisted workflows since Assisted isn't part of OCP.

    The periodic job needs to run on a frequent basis (e.g. daily) and have a `reporter_config` configured, in order to be notified on Slack whenever there's a breakage.

* In case the new job affects multiple repositories - every repository should have the same presubmit job so it could be tested for every component change.
For example, you can see that the `e2e-metal-assisted-olm` job is defined on several different repositories in this [link](https://steps.ci.openshift.org/search?job=e2e-metal-assisted-olm).

[An example of a PR adding a new job](https://github.com/openshift/release/pull/21604)

### FAQ

#### **How can I debug CI failures?**

A CI job can be debugged only in runtime.
Once a job terminates it can no longer be debugged because the cluster / machines used to run the job get torn down at the end of it.

However, each job produces artifacts such as (logs, SOS reports, must-gather logs) which can be used to try to analyze what went wrong in retrospect. Those artifacts can be accessed by going to the job artifacts.

You can follow the [OpenShift CI doc "Interact With Running Jobs"](https://docs.ci.openshift.org/docs/how-tos/interact-with-running-jobs/) guide or try to run the experimental [Debug Prow Jobs live](https://gist.github.com/omertuc/1ef4bdf22f0fedfbde46cf1feb149bb9) gist in order to connect to the OCP cluster running your prow tests. Contributions are welcome. :)

#### **If a CI job fails, where should I look for assisted-related failures?**

When a PR job fails there's a "details" button next to the GitHub context. It will show the [Spyglass](https://github.com/kubernetes/test-infra/tree/master/prow/spyglass) view. In there, you should look if there are other builds that failed recently for the same job using the "Job History" button.

#### **When is it ok to retest?**

Whenever there's a failure - first, you should look for its root cause before hitting the "/retest" command.
It should only be used when there's a known flaky issue.
Using the retest feature for no reason just wastes the project CI resources and money.

#### **How does the retest bot works?**

When a PR is ready to be merged (approved and not held) all the jobs will be retested for every new master that's being updated. In case any of the job fails - the openshift-bot will try to retest it automatically.
The retest job is defined under [infra-periodics.yaml](https://github.com/openshift/release/blob/c121e55f68fb37af41d7cd16877eaa79eeb972f1/ci-operator/jobs/infra-periodics.yaml#L202-L241)

#### **Where do these jobs run?**

Depends on the job.

* Single-stage tests (e.g. lint, unit tests) run inside of a scheduled container. [Read more](https://docs.ci.openshift.org/docs/architecture/ci-operator/#declaring-tests)
* Jobs that require a cluster (e.g. subsystem) run on a claimed OCP cluster from an hibernated pool of clusters.
[Read more](https://docs.ci.openshift.org/docs/architecture/ci-operator/#testing-with-a-cluster-from-a-cluster-pool)
* Baremetal jobs (i.e. e2e) run on a provisioned baremetal machine by [Equnix](https://www.equinix.nl/).

## How to run Assisted-service subsystem tests

More information is available here: [Assisted Installer Testing](/docs/dev/running-test.md).
