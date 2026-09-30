package panel

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func uploadDir(value string) string {
	if value != "" {
		return value
	}
	return filepath.Join(os.TempDir(), "mini-panel-uploads")
}

func appRoot(value string) string {
	if value != "" {
		return filepath.Clean(value)
	}
	return "/var/lib/mini-panel/apps"
}

func (a *App) saveUploadedFile(r *http.Request, fieldName, prefix string) (string, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", errors.New("file wajib diupload")
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 128<<20 {
		return "", errors.New("ukuran file harus lebih dari 0 dan maksimal 128 MB")
	}
	if err := os.MkdirAll(a.uploadDir, 0750); err != nil {
		return "", err
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	uploadPath := filepath.Join(a.uploadDir, prefix+token)
	out, err := os.OpenFile(uploadPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(uploadPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(uploadPath)
		return "", err
	}
	return uploadPath, nil
}
