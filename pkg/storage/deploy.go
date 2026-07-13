package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"mime"
	"path/filepath"
)

// DeployZip extracts a zip file in memory and streams its files to the cloud bucket under a specific site prefix.
func DeployZip(ctx context.Context, provider StorageProvider, bucket string, siteID string, zipData []byte) error {
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to parse zip archive: %w", err)
	}

	for _, file := range zipReader.File {
		// Skip directories as we only upload files to object storage
		if file.FileInfo().IsDir() {
			continue
		}

		err := func() error {
			rc, err := file.Open()
			if err != nil {
				return fmt.Errorf("failed to open file %s from zip: %w", file.Name, err)
			}
			defer rc.Close()

			// Construct the bucket object key (e.g., sites/my-portfolio/assets/index.js)
			destKey := fmt.Sprintf("sites/%s/%s", siteID, file.Name)
			contentType := DetectContentType(file.Name)

			intent := UploadIntent{
				Bucket:      bucket,
				Key:         destKey,
				ContentType: contentType,
			}

			// Stream upload directly to S3/GCS
			err = provider.UploadStream(ctx, intent, rc)
			if err != nil {
				return fmt.Errorf("failed to upload %s: %w", file.Name, err)
			}
			return nil
		}()

		if err != nil {
			return err
		}
	}

	return nil
}

// DetectContentType determines the correct MIME type based on file extension
// to ensure browsers render HTML, CSS, and JS correctly.
func DetectContentType(filename string) string {
	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}
