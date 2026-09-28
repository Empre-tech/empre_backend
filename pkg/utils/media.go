package utils

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
)

// allowedImageTypes are the still-image formats we accept everywhere
// (profile photo, banner, and gallery posts).
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// allowedVideoTypes are the short-video formats we accept for gallery posts
// only (not profile/banner, which are always still images).
var allowedVideoTypes = map[string]bool{
	"video/mp4":       true,
	"video/quicktime": true, // .mov, common from iPhones
	"video/webm":      true,
}

// sniff detects the real content type from the file's first bytes (never
// trusts the extension or the client-provided MIME type).
func sniff(fileHeader *multipart.FileHeader) (string, error) {
	f, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	defer f.Close()

	buffer := make([]byte, 512)
	_, err = f.Read(buffer)
	if err != nil {
		return "", fmt.Errorf("could not read file for validation: %w", err)
	}

	return http.DetectContentType(buffer), nil
}

// ValidateImage sniffs the first 512 bytes of a file to ensure it's a valid
// still image (JPEG/PNG/WebP). Used for profile photos and banners, which
// must always be images.
func ValidateImage(fileHeader *multipart.FileHeader) (string, error) {
	contentType, err := sniff(fileHeader)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(contentType, "image/") {
		return "", errors.New("uploaded file is not a valid image")
	}
	if !allowedImageTypes[contentType] {
		return "", errors.New("only JPEG, PNG and WebP are allowed")
	}

	return contentType, nil
}

// ValidateGalleryMedia is like ValidateImage but also accepts a short video
// (MP4/MOV/WebM), since gallery "publicaciones" can be a photo or a video.
func ValidateGalleryMedia(fileHeader *multipart.FileHeader) (string, error) {
	contentType, err := sniff(fileHeader)
	if err != nil {
		return "", err
	}

	if allowedImageTypes[contentType] || allowedVideoTypes[contentType] {
		return contentType, nil
	}

	return "", errors.New("only JPEG, PNG, WebP images or MP4, MOV, WebM videos are allowed")
}

// IsVideoContentType reports whether a media content type is a video, as
// opposed to a still image.
func IsVideoContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "video/")
}
