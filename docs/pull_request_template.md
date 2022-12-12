<!--
Please include a summary of the change and which issue is fixed. Please also include relevant motivation and context. List any dependencies that are required for this change.

You can refer to [Kubernetes community documentation] on writing good commit messages, which provides good tips and ideas.

Some PRs address specific issues. Please, refer to the [CONTRIBUTING] documentation for more
information on how to link a PR to an existing issue.

It's recommended to take a few extra minutes to provide more information about
how this code was tested. Here are some questions that may be worth answering:

- Should this PR be tested by the reviewer?
- Is this PR relying on CI for an e2e test run?
- Should this PR be tested in a specific environment?
- Any logs, screenshots, etc that can help with the review process?

-->

## List all the issues related to this PR

- [ ] New Feature <!-- new functionality -->
- [ ] Enhancement <!-- refactor, code changes, improvement, that won't add new features -->
- [ ] Bug fix
- [ ] Tests
- [ ] Documentation
- [ ] CI/CD <!-- Notice that changes for Dockerfiles/Jenkinsfiles aren't tested in CI due to a known bug. -->

## What environments does this code impact?

- [ ] Automation (CI, tools, etc)
- [ ] Cloud
- [ ] Operator Managed Deployments
- [x] None

## How was this code tested?

<!-- Please, select one or more if needed: -->

- [ ] assisted-test-infra environment
- [ ] dev-scripts environment
- [ ] Reviewer's test appreciated
- [ ] Waiting for CI to do a full test run
- [ ] Manual (Elaborate on how it was tested)
- [x] No tests needed

## Checklist

- [ ] Title and description added to both, commit and PR.
- [ ] Relevant issues have been associated (see [CONTRIBUTING] guide)
- [ ] This change does not require a documentation update (docstring, `docs`, README, etc)
- [ ] Does this change include unit-tests (note that code changes require unit-tests)

## Reviewers Checklist

- Are the title and description (in both PR and commit) meaningful and clear?
- Is there a bug required (and linked) for this change?
- Should this PR be backported?

[kubernetes community documentation]: https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#commit-message-guidelines
[contributing]: https://github.com/openshift/assisted-service/blob/master/CONTRIBUTING.md
