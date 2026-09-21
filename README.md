# gotexify

gotexify is a Go Azure Functions application that uploads an image to private Azure Blob Storage and extracts text with the Azure AI Vision Read API.

[![Deploy to Azure](https://aka.ms/deploytoazurebutton)](https://portal.azure.com/#create/Microsoft.Template/uri/https%3A%2F%2Fraw.githubusercontent.com%2Fwugreg%2Fgotexify%2Fmain%2Fazuredeploy.json)

## What the deployment creates

The button opens an Azure portal custom deployment for [`azuredeploy.json`](./azuredeploy.json). The template creates:

- an Azure Functions Flex Consumption app for the preview native Go worker;
- a system-assigned managed identity for the Function App;
- a storage account with private `uploads` and `deploymentpackage` containers;
- an Azure AI Vision account;
- Application Insights and a Log Analytics workspace; and
- least-privilege role assignments for storage, Vision, and Azure Monitor.

Shared-key access and public blob access are disabled. The application uses managed identity for Azure service authentication.

> Azure Functions support for Go is currently in public preview. The template uses runtime `go` version `1.0`, Linux Flex Consumption, and disables HTTP/2 as required by the preview.

## Deploy

### Provision from GitHub

1. Select **Deploy to Azure** above.
2. Choose a subscription and resource group.
3. Select a region that supports both [Flex Consumption](https://learn.microsoft.com/azure/azure-functions/flex-consumption-how-to#view-currently-supported-regions) and [Azure AI Vision](https://azure.microsoft.com/explore/global-infrastructure/products-by-region/).
4. Keep `packageUri` empty to provision only the infrastructure, or supply a public HTTPS URL to a deployment zip created by `func pack`.
5. Review and create the deployment.

When `packageUri` is supplied, the template uses the `Microsoft.Web/sites/extensions/onedeploy` resource to deploy the prebuilt package. Remote build is disabled because `func pack` produces a Linux x64 package that is ready to run.

### Publish after provisioning

If `packageUri` was left empty, use the `functionAppName` deployment output:

```powershell
func azure functionapp publish <FUNCTION_APP_NAME>
```

Azure Functions Core Tools 4.12 or later builds, packages, and publishes the Go application. Azure CLI 2.87.0 or later is required for current Go deployment flows.

To create a package for One Deploy instead:

```powershell
func pack
```

Upload the generated zip to a public HTTPS location, then redeploy the template with its URL as `packageUri`. Do not put credentials or SAS tokens in committed parameter files.

## Run locally

Prerequisites:

- Go 1.24 or later
- Azure Functions Core Tools 4.12 or later
- access to an Azure Storage account and an Azure AI Vision account

Set these values in `local.settings.json`:

- `STORAGE_ACCOUNT_NAME`
- `STORAGE_CONTAINER_NAME` (defaults to `uploads`)
- `AZURE_AI_VISION_ENDPOINT`

The app uses `ManagedIdentityCredential`, so authenticate through a managed identity when hosted in Azure.

Start the Functions host:

```powershell
func start
```

The registered HTTP routes are:

- `GET /api/hello`
- `POST /api/upload`
- `POST /api/extract`

## Template parameters

| Parameter | Default | Description |
| --- | --- | --- |
| `location` | Resource group location | Region for all Azure resources |
| `resourcePrefix` | `gotexify` | Lowercase resource-name prefix |
| `visionSku` | `S1` | Azure AI Vision pricing tier (`F0` or `S1`) |
| `packageUri` | Empty | Optional public URL of a zip produced by `func pack` |

The example values in [`azuredeploy.parameters.json`](./azuredeploy.parameters.json) contain no secrets.
