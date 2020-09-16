# Contribute to the Assisted Installer Project

## How to commit?

We enforce contributors behavior with commit messages to reference JIRA/GitHub issues.

### Why?

Organized history and to create a CHANGE LOG for each version.

### How does it work?

```bash
valid_commit_regex='^([A-Z]+-[0-9]+|#[0-9]+|merge|no-issue)'

error_msg="""Aborting commit.
Your commit message is missing a prefix of either a JIRA issue ('JIRA-1111'), a GitHub issue ('#39') or 'Merge'.
You can ignore the ticket by prefixing with 'NO-ISSUE'.

Your message is preserved at '${commit_file}'
"""
```

It will search for a commit message starting with a valid JIRA/GitHub issue notation or "NO-ISSUE" in case there's no ticket.
