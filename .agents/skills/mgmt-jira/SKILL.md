---
name: mgmt-jira
description: "Create and manage JIRA issues in the MGMT project for assisted-service. Use when the user asks to file, create, check, view, or update JIRA issues related to assisted-service development."
---

# MGMT JIRA Issue Management

This skill helps manage JIRA issues in the MGMT (KNI Management) project for assisted-service.

## Project Configuration

- **Project**: MGMT
- **Cloud ID**: 2b9e35e3-6bd3-4cec-b838-f4249ee02432
- **Default Component**: Assisted-Installer (ID: 49033)
- **JIRA URL**: https://redhat.atlassian.net

## Creating Issues

When the user asks to create a JIRA issue:

1. **Determine issue type** (default: Story)
   - Story (10009): Feature work, enhancements
   - Bug (10016): Problems or errors
   - Task (10014): Small discrete work items
   - Spike (10104): Research tasks
   - Epic (10000): Large stories to be broken down

2. **Use markdown format** for descriptions
   - Set `contentFormat: "markdown"` parameter
   - Use standard markdown: `##` for headings, backticks for code, `-` for bullets, `|---|---|` for tables
   - Do NOT use Jira wiki syntax (`h2.`, `{{code}}`, etc.)

3. **Set default component** to Assisted-Installer unless specified otherwise
   - Component ID: 49033
   - Pass as: `{"components": [{"id": "49033"}]}`

4. **Return the issue URL** after creation
   - Format: https://redhat.atlassian.net/browse/MGMT-XXXXX

### Example: Create issue from markdown file

```typescript
mcp__plugin_atlassian_atlassian__createJiraIssue({
  cloudId: "2b9e35e3-6bd3-4cec-b838-f4249ee02432",
  projectKey: "MGMT",
  issueTypeName: "Story",
  summary: "Issue title here",
  description: "## Summary\n\n...",
  contentFormat: "markdown",
  components: [{"id": "49033"}]
})
```

### Available Components

Common assisted-service components:
- **Assisted-Installer** (49033) - Default, general AI work
- **Assisted-Installer Service** (49094) - Service-specific issues
- **Assisted-Installer Agent** (49081) - Agent-specific issues
- **Assisted-Installer UI (OCM)** (49044) - UI in OCM/SaaS
- **Assisted-Installer CI** (49045) - CI/test infrastructure
- **Assisted-Installer CAPI** (49037) - CAPI integration


### Use the user's JIRA skill

Users can either have jira-cli or jira MCP.

## Viewing Issues

To check/view an issue:

```typescript
mcp__plugin_atlassian_atlassian__getJiraIssue({
  cloudId: "2b9e35e3-6bd3-4cec-b838-f4249ee02432",
  issueIdOrKey: "MGMT-23599"
})
```

## Updating Issues

To update an issue:

```typescript
mcp__plugin_atlassian_atlassian__editJiraIssue({
  cloudId: "2b9e35e3-6bd3-4cec-b838-f4249ee02432",
  issueIdOrKey: "MGMT-23599",
  contentFormat: "markdown",
  fields: {
    description: "## Updated description...",
    components: [{"id": "49033"}]
  }
})
```

## Searching Issues

To search for issues:

```typescript
mcp__plugin_atlassian_atlassian__searchJiraIssuesUsingJql({
  cloudId: "2b9e35e3-6bd3-4cec-b838-f4249ee02432",
  jql: "project = MGMT AND component = 'Assisted-Installer' AND status = 'To Do'"
})
```

## Common Workflows

### 1. File issue from documentation

When user says "create JIRA from docs/dev/issue.md":
1. Read the markdown file
2. Extract title (first heading or filename)
3. Use file content as description
4. Create with default component (Assisted-Installer)
5. Return URL

### 2. Update issue with component

When user says "add Assisted Installer component to MGMT-XXXXX":
1. Use editJiraIssue with `{"components": [{"id": "49033"}]}`

### 3. Check issue status

When user says "check MGMT-XXXXX":
1. Use getJiraIssue
2. Display: title, status, assignee, description summary

## Important Notes

- **Always use markdown format** (`contentFormat: "markdown"`) for new issues and updates
- **Default to Assisted-Installer component** (49033) unless user specifies otherwise
- **Return the issue URL** after creation so user can view it
- **Use Story type** as default unless context suggests Bug, Task, etc.
