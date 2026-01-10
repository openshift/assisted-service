# Contribute to the Assisted Installer Project

## Pull Request Template

Every PR should fill in the [PULL_REQUEST_TEMPLATE](https://github.com/openshift/assisted-service/blob/master/docs/pull_request_template.md) which is automatically proposed by GitHub when creating a new PR.

## How to commit?

We enforce contributors behavior with commit messages to reference JIRA/GitHub issues.

### Why?

Organized history and to create a CHANGE LOG for each version.

### How does it work?

[This script](https://github.com/openshift/assisted-service/blob/master/hack/check-commit-message.sh#L7) checks for a valid issue (JIRA or GitHub) reference and fails the build otherwise with the message

```text
Your commit message should start with a JIRA issue ('JIRA-1111') or a GitHub issue ('#39')
with a following colon(:).
i.e. 'MGMT-42: Summary of the commit message'
You can also ignore the ticket checking with 'NO-ISSUE' for master only.
```

It will search for a commit message containing a valid JIRA/GitHub issue notation or "NO-ISSUE" in case there's no ticket.


### Examples

1. JIRA reference without a Bugzilla link

    ```text
    MGMT-6075: Implement ResetHostValidation API call
    ```

1. No reference

    ```text
    NO-ISSUE: allow GitHub-created reverts to be used
    ```

1. GitHub reference

    ```text
    #1 Fixing the very first GitHub issue
    ```

    **NOTE**: The following is also correct

    ```text
    Fixing the very first GitHub issue

    [...]

    Closes: #1
    ```

### Best practices

See the [Kubernetes guidelines](https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#best-practices-for-faster-reviews)
Specifically:
1. [Smaller is better](https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#best-practices-for-faster-reviews)
2. [Squashing](https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#squashing)
3. [Commit message guidelines](https://github.com/kubernetes/community/blob/master/contributors/guide/pull-requests.md#try-to-keep-the-subject-line-to-50-characters-or-less-do-not-exceed-72-characters)

## Testing

More information is available here: [Assisted Installer Testing](docs/dev/testing.md)
