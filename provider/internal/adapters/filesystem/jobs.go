package filesystem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const maxJobBytes = 1 << 20

type JobStore struct {
	mu   sync.Mutex
	root string
}

func NewJobStore(root string) (*JobStore, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("artifact root must be a directory")
	}

	jobsPath := filepath.Join(root, ".jobs")
	jobsInfo, err := os.Lstat(jobsPath)
	if os.IsNotExist(err) {
		if err := os.Mkdir(jobsPath, 0o700); err != nil {
			return nil, fmt.Errorf("create job directory: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect job directory: %w", err)
	} else if jobsInfo.Mode()&os.ModeSymlink != 0 || !jobsInfo.IsDir() {
		return nil, fmt.Errorf("job directory must be a directory")
	}
	return &JobStore{root: root}, nil
}

func (store *JobStore) Create(job domain.Job) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateJob(job); err != nil {
		return err
	}
	path, err := store.jobPath(job.ID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect job file: %w", err)
	}
	return store.write(job)
}

func (store *JobStore) Get(id string) (domain.Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.read(id)
}

func (store *JobStore) List() ([]domain.Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(store.root, ".jobs"))
	if err != nil {
		return nil, fmt.Errorf("read job directory: %w", err)
	}
	jobs := make([]domain.Job, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("job directory contains symlink %q", entry.Name())
		}
		id := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		job, err := store.read(id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if !jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
		}
		return jobs[i].ID < jobs[j].ID
	})
	return jobs, nil
}

func (store *JobStore) Update(job domain.Job) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateJob(job); err != nil {
		return err
	}
	path, err := store.jobPath(job.ID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("inspect job file: %w", err)
	}
	return store.write(job)
}

func (store *JobStore) read(id string) (domain.Job, error) {
	path, err := store.jobPath(id)
	if err != nil {
		return domain.Job{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return domain.Job{}, err
	}
	if !info.Mode().IsRegular() {
		return domain.Job{}, fmt.Errorf("job file must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Job{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxJobBytes+1))
	if err != nil {
		return domain.Job{}, fmt.Errorf("read job %q: %w", id, err)
	}
	if len(data) > maxJobBytes {
		return domain.Job{}, fmt.Errorf("job %q exceeds %d bytes", id, maxJobBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var job domain.Job
	if err := decoder.Decode(&job); err != nil {
		return domain.Job{}, fmt.Errorf("decode job %q: %w", id, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.Job{}, fmt.Errorf("job %q contains trailing JSON", id)
	}
	if err := validateJob(job); err != nil {
		return domain.Job{}, fmt.Errorf("validate job %q: %w", id, err)
	}
	return job, nil
}

func (store *JobStore) write(job domain.Job) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job: %w", err)
	}
	data = append(data, '\n')
	return safefs.AtomicWrite(store.root, filepath.ToSlash(filepath.Join(".jobs", job.ID+".json")), data, 0o600)
}

func (store *JobStore) jobPath(id string) (string, error) {
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return "", fmt.Errorf("invalid job id")
	}
	return filepath.Join(store.root, ".jobs", id+".json"), nil
}

func validateJob(job domain.Job) error {
	if !protocol.ValidSiteName(job.ID) || len(job.ID) > 128 {
		return fmt.Errorf("invalid job id")
	}
	if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) {
		return fmt.Errorf("invalid job timestamps")
	}
	if !protocol.IsSHA256(job.InputHash) {
		return fmt.Errorf("invalid job input hash")
	}
	switch job.Status {
	case domain.JobStatusPlanned, domain.JobStatusBlocked, domain.JobStatusCancelled, domain.JobStatusFailed:
	default:
		return fmt.Errorf("invalid job status")
	}
	return nil
}
