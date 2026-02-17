package utils

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
)

// ValidateImage sniffs the first 512 bytes of a file to ensure it's a valid image.
// Returns the detected content type and an error if invalid.
func ValidateImage(fileHeader *multipart.FileHeader) (string, error) {
	f, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	defer f.Close()

	// Only need the first 512 bytes for sniffing
	buffer := make([]byte, 512)
	_, err = f.Read(buffer)
	if err != nil {
		return "", fmt.Errorf("could not read file for validation: %w", err)
	}

	contentType := http.DetectContentType(buffer)
	if !strings.HasPrefix(contentType, "image/") {
		return "", errors.New("uploaded file is not a valid image")
	}

	// Double check specific allowed images
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return "", errors.New("only JPEG, PNG and WebP are allowed")
	}

	return contentType, nil
}
