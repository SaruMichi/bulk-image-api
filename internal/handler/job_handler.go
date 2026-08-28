package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/SaruMichi/bulk-image-api/internal/repository"
	"github.com/SaruMichi/bulk-image-api/internal/service"
)

const (
	maxUploadMemory = 32 << 20 // 32MB buffer di memory, sisanya spill ke disk sementara
	maxFileSize     = 10 << 20 // 10MB per file
	maxFilesPerJob  = 20
)

var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

type JobHandler struct {
	service   *service.JobService
	uploadDir string // storage/original
}

func NewJobHandler(svc *service.JobService, uploadDir string) *JobHandler {
	return &JobHandler{service: svc, uploadDir: uploadDir}
}

// Routes mendaftarkan endpoint ke chi router.
func (h *JobHandler) Routes(r chi.Router) {
	r.Post("/jobs", h.Upload)
	r.Get("/jobs/{id}", h.GetJob)
}

// Upload menerima multipart/form-data dengan field "files" (bisa lebih dari satu),
// memvalidasi tipe & ukuran, menyimpan ke storage, lalu memproses SYNCHRONOUS.
func (h *JobHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
		writeError(w, http.StatusBadRequest, "gagal parse form: "+err.Error())
		return
	}

	fileHeaders := r.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		writeError(w, http.StatusBadRequest, "tidak ada file di field 'files'")
		return
	}
	if len(fileHeaders) > maxFilesPerJob {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("maksimal %d file per batch", maxFilesPerJob))
		return
	}

	jobUploadDir := filepath.Join(h.uploadDir, uuid.NewString())
	if err := os.MkdirAll(jobUploadDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "gagal menyiapkan storage")
		return
	}

	var saved []service.SavedFile
	for _, fh := range fileHeaders {
		if err := validateFileHeader(fh); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("file %q ditolak: %v", fh.Filename, err))
			return
		}

		savedPath, err := saveUploadedFile(fh, jobUploadDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("gagal menyimpan file %q: %v", fh.Filename, err))
			return
		}

		saved = append(saved, service.SavedFile{
			OriginalFilename: fh.Filename,
			OriginalPath:     savedPath,
		})
	}

	job, err := h.service.ProcessBatch(r.Context(), saved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gagal memproses batch: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

// GetJob mengembalikan detail job beserta seluruh job_files-nya.
func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id job tidak valid")
		return
	}

	job, files, err := h.service.GetJobDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job tidak ditemukan")
			return
		}
		writeError(w, http.StatusInternalServerError, "gagal mengambil job: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job":   job,
		"files": files,
	})
}

func validateFileHeader(fh *multipart.FileHeader) error {
	if fh.Size > maxFileSize {
		return fmt.Errorf("ukuran %d bytes melebihi batas %d bytes", fh.Size, maxFileSize)
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !allowedExtensions[ext] {
		return fmt.Errorf("ekstensi %q tidak diizinkan", ext)
	}
	return nil
}

func saveUploadedFile(fh *multipart.FileHeader, destDir string) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	ext := filepath.Ext(fh.Filename)
	destPath := filepath.Join(destDir, uuid.NewString()+ext)

	dst, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return destPath, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
