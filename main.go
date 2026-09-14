package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

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
	storageClient     *azblob.Client
	storageClientErr  error
	storageClientOnce sync.Once
)

type pageData struct {
	Message string
	Success bool
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
	renderPage(w, http.StatusCreated, pageData{Message: "Uploaded " + fileName + ".", Success: true})
}

func getStorageClient() (*azblob.Client, error) {
	storageClientOnce.Do(func() {
		accountName := os.Getenv("STORAGE_ACCOUNT_NAME")
		if accountName == "" {
			storageClientErr = fmt.Errorf("STORAGE_ACCOUNT_NAME is not set")
			return
		}

		credential, err := azidentity.NewManagedIdentityCredential(nil)
		if err != nil {
			storageClientErr = err
			return
		}

		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
		storageClient, storageClientErr = azblob.NewClient(serviceURL, credential, nil)
	})
	return storageClient, storageClientErr
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
	worker.Start(app)
}
