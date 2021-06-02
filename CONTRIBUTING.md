# Contribute to the Assisted Installer Project

## Template

Every PR should fill in the [PULL_REQUEST_TEMPLATE] which is automatically proposed by GitHub when creating a new PR.

## How to commit?

We enforce contributors behavior with commit messages to reference JIRA/GitHub issues.

### Why?

Organized history and to create a CHANGE LOG for each version.

### How does it work?

[This script](https://github.com/openshift/assisted-service/blob/master/tools/check-commit-message.sh#L7) checks for a valid issue (JIRA or GitHub) reference and fails the build otherwise with the message

```bash
Your commit message is missing either a JIRA issue ('JIRA-1111'), a GitHub issue ('#39').
You can also ignore the ticket checking with 'NO-ISSUE'.
```

It will search for a commit message containing a valid JIRA/GitHub issue notation or "NO-ISSUE" in case there's no ticket.

### Bugzilla references

The openshift-ci bot looks for `Bug XXX:` in the title of the pull request in order to reference the GitHub PR in the tracker.

### Examples

1. JIRA reference without a Bugzilla link

```
MGMT-6075 Implement ResetHostValidation API call
```

2. No reference

```
NO-ISSUE allow GitHub-created reverts to be used
```

3. Bugzilla and JIRA reference

```
Bug 1957227: Allow overriding defaults via provided ConfigMap

[...]

Closes: OCPBUGSM-28781
```

**NOTE**

For this commit the following is also correct, but the PR is not automatically linked with the bug tracker. Linking only to Bugzilla without referencing JIRA is not correct.

```
OCPBUGSM-28781 Allow overriding defaults via provided ConfigMap
```

4. GitHub reference

```
#1 Fixing the very first GitHub issue
```

**NOTE**

The following is also correct

```
Fixing the very first GitHub issue

[...]

Closes: #1
```


[PULL_REQUEST_TEMPLATE]: https://github.com/openshift/assisted-service/blob/master/docs/pull_request_template.md
