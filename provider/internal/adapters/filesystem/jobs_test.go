package filesystem

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

func TestJobStorePersistsListsAndUpdatesJobs(t *testing.T) {
	root := t.TempDir()
	store, err := NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob("job-first")
	if err := store.Create(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(job); !os.IsExist(err) {
		t.Fatalf("expected duplicate job rejection, got %v", err)
	}
	loaded, err := store.Get(job.ID)
	if err != nil || loaded.ID != job.ID {
		t.Fatalf("job was not loaded: %#v, %v", loaded, err)
	}
	job.Status = domain.JobStatusCancelled
	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	if err := store.Update(job); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.List()
	if err != nil || len(jobs) != 1 || jobs[0].Status != domain.JobStatusCancelled {
		t.Fatalf("unexpected stored jobs: %#v, %v", jobs, err)
	}
	info, err := os.Stat(filepath.Join(root, ".jobs", job.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("job file should be private: %v, %v", info, err)
	}
}

func TestJobStoreRejectsCorruptAndUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".jobs", "job-bad.json"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("expected corrupt job file to be rejected")
	}

	unsafeRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafeRoot, ".jobs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewJobStore(unsafeRoot); err == nil {
		t.Fatal("expected symlink job directory to be rejected")
	}
}

func testJob(id string) domain.Job {
	now := time.Now().UTC()
	return domain.Job{
		ID: id, CreatedAt: now, UpdatedAt: now,
		InputHash: protocol.SHA256([]byte("input")), Status: domain.JobStatusPlanned,
		Plan: domain.Plan{Version: "v1", Status: "ready"},
	}
}
