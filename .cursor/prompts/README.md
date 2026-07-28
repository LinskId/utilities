# Cursor Prompt Templates

These prompt templates help you quickly invoke common actions in Cursor.

## How to Use

In Cursor, type `@` followed by the prompt name to include it in your conversation:

- `@deploy-dcm` - Deploy the full DCM stack
- `@tear-down` - Tear down a running DCM deployment
- `@check-versions` - Get running container versions
- `@troubleshoot-deploy` - Diagnose deployment issues
- `@maintain-pr-summary` - Maintain a running PR summary document

## Available Prompts

| Prompt | Purpose |
|--------|---------| 
| `deploy-dcm.md` | Deploy the full DCM stack via podman-compose |
| `tear-down.md` | Stop containers, remove volumes, clean up |
| `check-versions.md` | Resolve running containers to git commit SHAs |
| `troubleshoot-deploy.md` | Diagnose common deployment failures |
| `maintain-pr-summary.md` | Maintain a running PR summary as work is developed |
| `write-test-plan.md` | Write a comprehensive E2E test plan for a Jira ticket |

## Example Usage

1. **Deploy**: Type `@deploy-dcm` then ask "Deploy the DCM stack from the feature-x branch"
2. **Tear down**: Type `@tear-down` then ask "Clean up the deployment"
3. **Versions**: Type `@check-versions` then ask "What versions are running?"
4. **Troubleshoot**: Type `@troubleshoot-deploy` then paste your error output
5. **PR Summary**: Type `@maintain-pr-summary` then ask "Update the PR summary with recent changes"
6. **Test Plan**: Type `@write-test-plan` then ask "Write a test plan for FLPATH-XXXX"

## For Claude Code / claude.ai

These prompts are also useful outside Cursor. Reference the relevant prompt content
in your conversation to provide context for your request.

See also: `CLAUDE.md` at project root for consolidated project context.
