package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/azure/azure-functions-golang-worker/sdk"
	"github.com/azure/azure-functions-golang-worker/worker"
)

const (
	defaultContainerName = "uploads"
	maxUploadSize        = 10 << 20
	maxRequestSize       = maxUploadSize + (1 << 20)
)

//go:embed upload.html
var pageFiles embed.FS

var uploadPage = template.Must(template.ParseFS(pageFiles, "upload.html"))

var (
	azureCredential     *azidentity.ManagedIdentityCredential
	azureCredentialErr  error
	azureCredentialOnce sync.Once
	storageClient       *azblob.Client
	storageClientErr    error
	storageClientOnce   sync.Once
)

type pageData struct {
	Message       string
	Success       bool
	BlobName      string
	ExtractedText string
}

type readOperation struct {
	Status        string `json:"status"`
	AnalyzeResult struct {
		ReadResults []struct {
			Lines []struct {
				Text string `json:"text"`
			} `json:"lines"`
		} `json:"readResults"`
	} `json:"analyzeResult"`
}

func HTTPTriggerHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Processing HTTP Trigger for %s", r.URL.Path)
	renderPage(w, http.StatusOK, pageData{})
}

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	file, header, err := r.FormFile("document")
	if err != nil {
		renderPage(w, http.StatusBadRequest, pageData{Message: "Select a text file smaller than 10 MB."})
		return
	}
	defer file.Close()

	if header.Size > maxUploadSize {
		renderPage(w, http.StatusRequestEntityTooLarge, pageData{Message: "The selected file exceeds the 10 MB limit."})
		return
	}

	fileName := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if fileName == "." || fileName == "" {
		renderPage(w, http.StatusBadRequest, pageData{Message: "The selected file has an invalid name."})
		return
	}

	client, err := getStorageClient()
	if err != nil {
		log.Printf("Configuring Blob Storage client: %v", err)
		renderPage(w, http.StatusInternalServerError, pageData{Message: "Storage is not configured."})
		return
	}

	containerName := os.Getenv("STORAGE_CONTAINER_NAME")
	if containerName == "" {
		containerName = defaultContainerName
	}
	blobName := fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), fileName)
	if _, err := client.UploadStream(r.Context(), containerName, blobName, file, nil); err != nil {
		log.Printf("Uploading %q to container %q: %v", blobName, containerName, err)
		renderPage(w, http.StatusBadGateway, pageData{Message: "The file could not be saved to storage."})
		return
	}

	log.Printf("Uploaded blob %q to container %q", blobName, containerName)
	renderPage(w, http.StatusCreated, pageData{
		Message:  "Uploaded " + fileName + ".",
		Success:  true,
		BlobName: blobName,
	})
}

func ExtractHandler(w http.ResponseWriter, r *http.Request) {
	slog.InfoContext(r.Context(), "Starting text extraction for request")
	blobName := r.FormValue("blobName")
	if blobName == "" || len(blobName) > 1024 || blobName != path.Base(blobName) || strings.Contains(blobName, "\\") {
		renderPage(w, http.StatusBadRequest, pageData{Message: "The uploaded image reference is invalid."})
		return
	}

	slog.InfoContext(r.Context(), "Extracting text for blob", slog.String("blobName", blobName))
	containerName := os.Getenv("STORAGE_CONTAINER_NAME")
	if containerName == "" {
		containerName = defaultContainerName
	}

	client, err := getStorageClient()
	if err != nil {
		//log.Printf("Configuring Blob Storage client: %v", err)
		slog.InfoContext(r.Context(), "Configuring Blob Storage client failed", slog.Any("error", err))
		renderPage(w, http.StatusInternalServerError, pageData{Message: "Storage is not configured."})
		return
	}
	download, err := client.DownloadStream(r.Context(), containerName, blobName, nil)
	if err != nil {
		//log.Printf("Downloading blob %q from container %q: %v", blobName, containerName, err)
		slog.InfoContext(r.Context(), "Failed to download blob", slog.String("blobName", blobName))
		renderPage(w, http.StatusBadGateway, pageData{
			Message:  "The image could not be loaded from storage.",
			BlobName: blobName,
		})
		return
	}
	defer download.Body.Close()

	imageBytes, err := io.ReadAll(io.LimitReader(download.Body, maxUploadSize+1))
	if err != nil {
		//log.Printf("Reading blob %q from container %q: %v", blobName, containerName, err)
		slog.InfoContext(r.Context(), "Failed to read blob", slog.String("blobName", blobName), slog.Any("error", err))
		renderPage(w, http.StatusBadGateway, pageData{
			Message:  "The image could not be loaded from storage.",
			BlobName: blobName,
		})
		return
	}
	if len(imageBytes) > maxUploadSize {
		renderPage(w, http.StatusRequestEntityTooLarge, pageData{
			Message:  "The stored image exceeds the 10 MB limit.",
			BlobName: blobName,
		})
		return
	}

	//log.Printf("Running OCR extraction for blob %q", blobName)
	slog.InfoContext(r.Context(), "Running OCR extraction for blob", slog.String("blobName", blobName))
	extractedText, err := extractText(r.Context(), imageBytes)
	if err != nil {
		//log.Printf("Extracting text from blob %q: %v", blobName, err)
		slog.InfoContext(r.Context(), "Failed to extract text from blob", slog.String("blobName", blobName), slog.Any("error", err))
		renderPage(w, http.StatusBadGateway, pageData{
			Message:  "Text could not be extracted from the image.",
			BlobName: blobName,
		})
		return
	}

	message := "Text extracted successfully."
	if extractedText == "" {
		message = "OCR completed, but no text was found."
	}
	renderPage(w, http.StatusOK, pageData{
		Message:       message,
		Success:       true,
		BlobName:      blobName,
		ExtractedText: extractedText,
	})
}

func extractText(ctx context.Context, imageBytes []byte) (string, error) {
	endpoint := strings.TrimRight(os.Getenv("AZURE_AI_VISION_ENDPOINT"), "/")
	if endpoint == "" {
		return "", fmt.Errorf("AZURE_AI_VISION_ENDPOINT must be set")
	}

	credential, err := getAzureCredential()
	if err != nil {
		return "", fmt.Errorf("configuring Azure credential: %w", err)
	}
	token, err := credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://cognitiveservices.azure.com/.default"},
	})
	if err != nil {
		return "", fmt.Errorf("authenticating to Azure AI Vision: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/vision/v3.2/read/analyze", bytes.NewReader(imageBytes))
	if err != nil {
		return "", fmt.Errorf("creating OCR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Authorization", "Bearer "+token.Token)

	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("starting OCR operation: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("starting OCR operation returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	operationURL := response.Header.Get("Operation-Location")
	if operationURL == "" {
		return "", fmt.Errorf("OCR response did not include Operation-Location")
	}

	for attempt := 0; attempt < 30; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}

		operation, err := getReadOperation(ctx, client, operationURL, token.Token)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(operation.Status) {
		case "succeeded":
			var lines []string
			for _, result := range operation.AnalyzeResult.ReadResults {
				for _, line := range result.Lines {
					lines = append(lines, line.Text)
				}
			}
			return strings.Join(lines, "\n"), nil
		case "failed":
			return "", fmt.Errorf("OCR operation failed")
		}
	}

	return "", fmt.Errorf("OCR operation timed out")
}

func getReadOperation(ctx context.Context, client *http.Client, operationURL, accessToken string) (readOperation, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, operationURL, nil)
	if err != nil {
		return readOperation{}, fmt.Errorf("creating OCR status request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := client.Do(request)
	if err != nil {
		return readOperation{}, fmt.Errorf("checking OCR operation: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return readOperation{}, fmt.Errorf("checking OCR operation returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var operation readOperation
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&operation); err != nil {
		return readOperation{}, fmt.Errorf("decoding OCR operation: %w", err)
	}
	return operation, nil
}

func getStorageClient() (*azblob.Client, error) {
	storageClientOnce.Do(func() {
		accountName := os.Getenv("STORAGE_ACCOUNT_NAME")
		if accountName == "" {
			storageClientErr = fmt.Errorf("STORAGE_ACCOUNT_NAME is not set")
			return
		}

		credential, err := getAzureCredential()
		if err != nil {
			storageClientErr = err
			return
		}

		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
		storageClient, storageClientErr = azblob.NewClient(serviceURL, credential, nil)
	})
	return storageClient, storageClientErr
}

func getAzureCredential() (*azidentity.ManagedIdentityCredential, error) {
	azureCredentialOnce.Do(func() {
		azureCredential, azureCredentialErr = azidentity.NewManagedIdentityCredential(nil)
	})
	return azureCredential, azureCredentialErr
}

func renderPage(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := uploadPage.Execute(w, data); err != nil {
		log.Printf("Rendering upload page: %v", err)
	}
}

func main() {
	app := sdk.FunctionApp()
	app.HTTP("hello", HTTPTriggerHandler,
		sdk.WithMethods("GET"),
		sdk.WithAuth("anonymous"),
	)
	app.HTTP("upload", UploadHandler,
		sdk.WithMethods("POST"),
		sdk.WithAuth("anonymous"),
	)
	app.HTTP("extract", ExtractHandler,
		sdk.WithMethods("POST"),
		sdk.WithAuth("anonymous"),
	)
	worker.Start(app)
}
