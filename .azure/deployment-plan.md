# Deployment Plan

Status: Validated

## Goal

Create an Azure Resource Manager template for the gotexify Go Azure Functions application and add a GitHub-rendered **Deploy to Azure** button.

## Current Application

- Go custom-handler Azure Functions app
- HTTP endpoints: `hello`, `upload`, and `extract`
- Private Blob Storage container for uploaded images
- Azure AI Vision Read API for OCR
- System-assigned managed identity for Blob Storage and Vision authentication

## Proposed Resources

- Storage account and private `uploads` blob container
- Azure Functions Flex Consumption plan (`FC1`)
- Linux Function App configured for the preview native Go worker
- Application Insights and Log Analytics workspace
- Azure AI Vision/Cognitive Services account
- System-assigned managed identity on the Function App
- RBAC assignments for Blob Storage data access and Cognitive Services access

## Proposed Artifacts

- `azuredeploy.json`: resource-group ARM deployment template with parameters and non-secret outputs
- `azuredeploy.parameters.json`: non-secret example parameters
- `README.md`: architecture, prerequisites, deployment behavior, and GitHub **Deploy to Azure** button

## Application Delivery

Azure cannot compile files from the local checkout during an ARM deployment. The portal button will provision infrastructure, and application code delivery will use one of these options:

1. a public package URL supplied as an ARM parameter, allowing ARM to configure package deployment; or
2. a separate post-provisioning publish step such as `func azure functionapp publish` or GitHub Actions.

Recommended: make `packageUri` optional. With no URI, the button provisions infrastructure and README documents the publish command. With a URI, ARM invokes One Deploy for the prebuilt Linux package. No credentials are embedded.

## Security

- No passwords, keys, SAS tokens, or connection strings in committed files
- Managed identity and least-privilege RBAC
- HTTPS-only and TLS 1.2 minimum
- Public blob access disabled
- Storage shared-key access disabled; Functions host, deployment, and application data use managed identity
- RBAC: Storage Blob Data Owner, Storage Table Data Contributor, Cognitive Services User, and Monitoring Metrics Publisher
- Sensitive deployment outputs omitted

## Role Assignment Verification

- Status: Verified
- Identity checked: Function App system-assigned managed identity
- Storage account: Storage Blob Data Owner for Functions host state, package deployment, and application blob read/write
- Storage account: Storage Table Data Contributor for identity-based Functions host storage
- Azure AI Vision account: Cognitive Services User for Microsoft Entra token-based OCR requests
- Application Insights: Monitoring Metrics Publisher for Microsoft Entra-authenticated telemetry
- Scope: every assignment is limited to its target resource
- Local development: no user role is provisioned because the application explicitly uses `ManagedIdentityCredential`
- Issues: none

## Validation

- [x] All applicable validation checks pass
  - [x] Core local validation (ARM JSON parsing and ARM-to-Bicep build)
  - [x] Linting (no Bicep build errors)
  - [x] Azure Policy validation (not applicable to the subscription-neutral public template)

- ARM JSON and parameter JSON parse successfully.
- ARM template decompiles and rebuilds successfully with Bicep.
- Resource dependencies, API versions, role IDs, and expressions were checked against current Microsoft guidance and samples.
- `go test ./...` passes.
- `func pack` produces a Linux x64 package containing `app` and `host.json`.
- Deployment artifacts contain no embedded credentials.

## Section 7: Validation Proof

| Check | Command or method | Result |
| --- | --- | --- |
| ARM JSON | PowerShell `ConvertFrom-Json` for template and parameters | Passed; 14 resources parsed |
| ARM expressions and schemas | `az bicep decompile` followed by `az bicep build` in an isolated validation directory | Passed |
| Go tests and compilation | `go test ./...` | Passed; no test files and package compiled |
| Functions package | `func pack` with `FUNCTIONS_WORKER_RUNTIME=go`, followed by zip inspection | Passed; Linux x64 package contains `app` and `host.json` |
| Credentials | Targeted scan of deployment artifacts | Passed; no embedded password, account key, SAS token, or connection string |
| Git diff | `git diff --check` | Passed |
| RBAC | Static review against application storage, OCR, and telemetry operations | Passed |
| Subscription policy and what-if | Not run | Not applicable because the public template is intentionally subscription-neutral and no deployment target was selected |

## Approval

Approved by the user. The template remains subscription-neutral so the public GitHub button can be used across Azure subscriptions.
