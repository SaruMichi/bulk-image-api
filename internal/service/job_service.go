package service

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/google/uuid"

	"github.com/SaruMichi/bulk-image-api/internal/model"
	"github.com/SaruMichi/bulk-image-api/internal/repository"
	"github.com/SaruMichi/bulk-image-api/pkg/imageutil"
)

// SavedFile merepresentasikan file yang sudah divalidasi & disimpan
// ke storage oleh handler, siap diproses.
type SavedFile struct {
	OriginalFilename string
	OriginalPath     string
}

type JobService struct {
	repo          *repository.JobRepository
	resultDir     string
	maxWidth      int
	watermarkPath string // kosong = skip watermark
}

func NewJobService(repo *repository.JobRepository, resultDir string, maxWidth int, watermarkPath string) *JobService {
	return &JobService{
		repo:          repo,
		resultDir:     resultDir,
		maxWidth:      maxWidth,
		watermarkPath: watermarkPath,
	}
}

// ProcessBatch membuat satu job baru lalu memproses file-filenya secara
// PARALEL pakai worker pool: beberapa goroutine ("worker") jalan bareng,
// masing-masing ambil satu file dari channel, proses, ambil lagi -- sampai
// channel-nya habis.
//
// Jumlah worker mengikuti jumlah CPU core (runtime.NumCPU()), karena proses
// gambar itu CPU-heavy (resize/watermark) -- worker lebih banyak dari core
// yang ada justru bikin CPU rebutan, bukan lebih cepat.
//
// File yang gagal diproses langsung ditandai 'failed' dengan error_message
// terisi -- TIDAK ada retry otomatis. Retry = upload ulang dari user.
func (s *JobService) ProcessBatch(ctx context.Context, files []SavedFile) (*model.Job, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("tidak ada file untuk diproses")
	}

	job, err := s.repo.CreateJob(ctx, len(files))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat job: %w", err)
	}

	if err := s.repo.UpdateJobStatus(ctx, job.ID, model.JobStatusProcessing); err != nil {
		return nil, fmt.Errorf("gagal set status processing: %w", err)
	}

	numWorkers := runtime.NumCPU()
	if numWorkers > len(files) {
		// Nggak perlu buka lebih banyak worker daripada file yang ada.
		numWorkers = len(files)
	}

	// Buffered channel seukuran jumlah file: semua file langsung dimasukkan,
	// worker tinggal ambil satu-satu sampai channel ditutup & kosong.
	fileChan := make(chan SavedFile, len(files))
	for _, f := range files {
		fileChan <- f
	}
	close(fileChan)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range fileChan {
				s.processOneFile(ctx, job.ID, f)
			}
		}()
	}
	wg.Wait() // tunggu semua worker selesai sebelum lanjut

	if err := s.repo.TryFinishJob(ctx, job.ID); err != nil {
		return nil, fmt.Errorf("gagal finalize job: %w", err)
	}

	finalJob, err := s.repo.GetJobByID(ctx, job.ID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil job final: %w", err)
	}
	return finalJob, nil
}

// processOneFile menangani satu file: buat record job_file, proses gambar,
// lalu tandai done/failed. Error di sini TIDAK menghentikan file lain dalam
// batch yang sama -- setiap file independen.
func (s *JobService) processOneFile(ctx context.Context, jobID uuid.UUID, f SavedFile) {
	jf, err := s.repo.CreateJobFile(ctx, jobID, f.OriginalFilename, f.OriginalPath)
	if err != nil {
		// Kalau record job_file saja gagal dibuat, catat log dan lanjut ke file berikutnya.
		// jobs.total_files tetap seperti semula sehingga job tidak akan pernah
		// 'selesai' otomatis -- ini kasus jarang (biasanya DB down), butuh investigasi manual.
		log.Printf("gagal membuat job_file untuk %q (job %s): %v", f.OriginalFilename, jobID, err)
		return
	}

	if err := s.repo.UpdateJobFileStatusProcessing(ctx, jf.ID); err != nil {
		log.Printf("gagal set job_file %s ke processing: %v", jf.ID, err)
	}

	destDir := filepath.Join(s.resultDir, jobID.String())
	result, err := imageutil.Process(f.OriginalPath, destDir, imageutil.ProcessOptions{
		MaxWidth:      s.maxWidth,
		WatermarkPath: s.watermarkPath,
	})
	if err != nil {
		if markErr := s.repo.MarkJobFileFailed(ctx, jf.ID, err.Error()); markErr != nil {
			log.Printf("gagal menandai job_file %s sebagai failed: %v", jf.ID, markErr)
		}
		return
	}

	if err := s.repo.MarkJobFileDone(ctx, jf.ID, result.OutputPath); err != nil {
		log.Printf("gagal menandai job_file %s sebagai done: %v", jf.ID, err)
	}
}

// GetJobDetail mengambil job beserta seluruh job_files-nya.
func (s *JobService) GetJobDetail(ctx context.Context, jobID uuid.UUID) (*model.Job, []model.JobFile, error) {
	job, err := s.repo.GetJobByID(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.repo.ListJobFilesByJobID(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}
	return job, files, nil
}
