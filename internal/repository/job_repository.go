package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SaruMichi/bulk-image-api/internal/model"
)

var ErrNotFound = errors.New("record tidak ditemukan")

type JobRepository struct {
	db *pgxpool.Pool
}

func NewJobRepository(db *pgxpool.Pool) *JobRepository {
	return &JobRepository{db: db}
}

// CreateJob membuat record job baru dengan status pending.
func (r *JobRepository) CreateJob(ctx context.Context, totalFiles int) (*model.Job, error) {
	const q = `
		INSERT INTO jobs (total_files)
		VALUES ($1)
		RETURNING id, status, total_files, processed_files, failed_files, error_message, created_at, updated_at
	`
	var j model.Job
	err := r.db.QueryRow(ctx, q, totalFiles).Scan(
		&j.ID, &j.Status, &j.TotalFiles, &j.ProcessedFiles, &j.FailedFiles,
		&j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat job: %w", err)
	}
	return &j, nil
}

// GetJobByID mengambil satu job berdasarkan ID.
func (r *JobRepository) GetJobByID(ctx context.Context, id uuid.UUID) (*model.Job, error) {
	const q = `
		SELECT id, status, total_files, processed_files, failed_files, error_message, created_at, updated_at
		FROM jobs WHERE id = $1
	`
	var j model.Job
	err := r.db.QueryRow(ctx, q, id).Scan(
		&j.ID, &j.Status, &j.TotalFiles, &j.ProcessedFiles, &j.FailedFiles,
		&j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("gagal mengambil job: %w", err)
	}
	return &j, nil
}

// UpdateJobStatus mengubah status job (mis. pending -> processing -> done/failed).
func (r *JobRepository) UpdateJobStatus(ctx context.Context, id uuid.UUID, status model.JobStatus) error {
	const q = `UPDATE jobs SET status = $2, updated_at = now() WHERE id = $1`
	tag, err := r.db.Exec(ctx, q, id, status)
	if err != nil {
		return fmt.Errorf("gagal update status job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateJobFile membuat record job_files baru dengan status pending.
func (r *JobRepository) CreateJobFile(ctx context.Context, jobID uuid.UUID, originalFilename, originalPath string) (*model.JobFile, error) {
	const q = `
		INSERT INTO job_files (job_id, original_filename, original_path)
		VALUES ($1, $2, $3)
		RETURNING id, job_id, original_filename, original_path, result_path, status, error_message, created_at, updated_at
	`
	var jf model.JobFile
	err := r.db.QueryRow(ctx, q, jobID, originalFilename, originalPath).Scan(
		&jf.ID, &jf.JobID, &jf.OriginalFilename, &jf.OriginalPath, &jf.ResultPath,
		&jf.Status, &jf.ErrorMessage, &jf.CreatedAt, &jf.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat job_file: %w", err)
	}
	return &jf, nil
}

// ListJobFilesByJobID mengambil semua job_files milik satu job.
func (r *JobRepository) ListJobFilesByJobID(ctx context.Context, jobID uuid.UUID) ([]model.JobFile, error) {
	const q = `
		SELECT id, job_id, original_filename, original_path, result_path, status, error_message, created_at, updated_at
		FROM job_files WHERE job_id = $1 ORDER BY created_at ASC
	`
	rows, err := r.db.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil job_files: %w", err)
	}
	defer rows.Close()

	var files []model.JobFile
	for rows.Next() {
		var jf model.JobFile
		if err := rows.Scan(
			&jf.ID, &jf.JobID, &jf.OriginalFilename, &jf.OriginalPath, &jf.ResultPath,
			&jf.Status, &jf.ErrorMessage, &jf.CreatedAt, &jf.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("gagal scan job_file: %w", err)
		}
		files = append(files, jf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

// UpdateJobFileStatusProcessing menandai satu job_file mulai diproses.
func (r *JobRepository) UpdateJobFileStatusProcessing(ctx context.Context, jobFileID uuid.UUID) error {
	const q = `UPDATE job_files SET status = $2, updated_at = now() WHERE id = $1`
	tag, err := r.db.Exec(ctx, q, jobFileID, model.JobStatusProcessing)
	if err != nil {
		return fmt.Errorf("gagal update status job_file: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkJobFileDone menandai satu job_file selesai sukses, sekaligus
// menaikkan jobs.processed_files secara atomic (bukan read-then-write).
func (r *JobRepository) MarkJobFileDone(ctx context.Context, jobFileID uuid.UUID, resultPath string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("gagal begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const updateFile = `
		UPDATE job_files
		SET status = $2, result_path = $3, updated_at = now()
		WHERE id = $1
		RETURNING job_id
	`
	var jobID uuid.UUID
	if err := tx.QueryRow(ctx, updateFile, jobFileID, model.JobStatusDone, resultPath).Scan(&jobID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("gagal update job_file: %w", err)
	}

	const incrementJob = `
		UPDATE jobs
		SET processed_files = processed_files + 1, updated_at = now()
		WHERE id = $1
	`
	if _, err := tx.Exec(ctx, incrementJob, jobID); err != nil {
		return fmt.Errorf("gagal increment processed_files: %w", err)
	}

	return tx.Commit(ctx)
}

// MarkJobFileFailed menandai satu job_file gagal (tanpa retry otomatis),
// sekaligus menaikkan jobs.processed_files & jobs.failed_files secara atomic.
func (r *JobRepository) MarkJobFileFailed(ctx context.Context, jobFileID uuid.UUID, errMsg string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("gagal begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const updateFile = `
		UPDATE job_files
		SET status = $2, error_message = $3, updated_at = now()
		WHERE id = $1
		RETURNING job_id
	`
	var jobID uuid.UUID
	if err := tx.QueryRow(ctx, updateFile, jobFileID, model.JobStatusFailed, sql.NullString{String: errMsg, Valid: true}).Scan(&jobID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("gagal update job_file: %w", err)
	}

	const incrementJob = `
		UPDATE jobs
		SET processed_files = processed_files + 1, failed_files = failed_files + 1, updated_at = now()
		WHERE id = $1
	`
	if _, err := tx.Exec(ctx, incrementJob, jobID); err != nil {
		return fmt.Errorf("gagal increment processed_files/failed_files: %w", err)
	}

	return tx.Commit(ctx)
}

// TryFinishJob menandai job sebagai 'done' HANYA jika semua file
// sudah selesai diproses (processed_files >= total_files).
// Ingat: 'done' di sini berarti "selesai diproses", bukan "semua sukses" -
// cek jobs.failed_files terpisah untuk tahu ada file yang gagal atau tidak.
func (r *JobRepository) TryFinishJob(ctx context.Context, jobID uuid.UUID) error {
	const q = `
		UPDATE jobs
		SET status = $2, updated_at = now()
		WHERE id = $1 AND processed_files >= total_files
	`
	_, err := r.db.Exec(ctx, q, jobID, model.JobStatusDone)
	if err != nil {
		return fmt.Errorf("gagal finalize job: %w", err)
	}
	return nil
}
