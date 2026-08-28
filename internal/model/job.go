package model

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// JobStatus merepresentasikan status sebuah job maupun job_file.
// Nilai yang valid: pending, processing, done, failed.
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusDone       JobStatus = "done"
	JobStatusFailed     JobStatus = "failed"
)

// Job merepresentasikan satu batch upload (level jobs).
type Job struct {
	ID             uuid.UUID
	Status         JobStatus
	TotalFiles     int
	ProcessedFiles int
	FailedFiles    int
	ErrorMessage   sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobFile merepresentasikan satu file individual di dalam sebuah job.
type JobFile struct {
	ID               uuid.UUID
	JobID            uuid.UUID
	OriginalFilename string
	OriginalPath     string
	ResultPath       sql.NullString
	Status           JobStatus
	ErrorMessage     sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsFinished mengembalikan true jika job sudah selesai diproses
// (semua file sudah done atau failed), terlepas dari hasil akhirnya.
func (j *Job) IsFinished() bool {
	return j.Status == JobStatusDone || j.Status == JobStatusFailed
}
