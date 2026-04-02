package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dhawalhost/vibe-agents/internal/agents"
	vibecontext "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/output"
)

const eventBufSize = 256

// JobStatus represents the lifecycle state of a generation job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobComplete  JobStatus = "complete"
	JobFailed    JobStatus = "failed"
	JobIterating JobStatus = "iterating"
)

// Job holds all runtime state for one generation run.
type Job struct {
	ID          string                      `json:"id"`
	Status      JobStatus                   `json:"status"`
	Prompt      string                      `json:"prompt"`
	Provider    string                      `json:"provider"`
	Model       string                      `json:"model"`
	OutputDir   string                      `json:"output_dir"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
	Error       string                      `json:"error,omitempty"`
	SharedCtx   *vibecontext.SharedContext  `json:"-"`
	Orch        *agents.OrchestratorAgent   `json:"-"`
	events      chan vibecontext.Event
	ctx         context.Context
	cancel      context.CancelFunc
}

// JobStore is a thread-safe in-memory store for all jobs.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func newJobStore() *JobStore {
	return &JobStore{jobs: make(map[string]*Job)}
}

func (s *JobStore) add(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
}

func (s *JobStore) get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// GenerateRequest is the body for POST /api/generate.
type GenerateRequest struct {
	Prompt    string `json:"prompt"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	OutputDir string `json:"output_dir,omitempty"`
}

// IterateRequest is the body for POST /api/jobs/{id}/iterate.
type IterateRequest struct {
	Feedback string `json:"feedback"`
}

// fileListEntry is one entry in the file listing response.
type fileListEntry struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

// handleGenerate starts a new generation job.
func (srv *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	if req.Provider == "" {
		req.Provider = srv.defaultProvider
	}
	if req.Model == "" {
		req.Model = srv.defaultModel
	}
	if req.OutputDir == "" {
		req.OutputDir = "./output"
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixNano())
	jobCtx, jobCancel := context.WithCancel(context.Background())

	eventCh := make(chan vibecontext.Event, eventBufSize)

	sharedCtx := vibecontext.New(req.Prompt)
	sharedCtx.OutputDir = req.OutputDir
	sharedCtx.EventBus = eventCh

	orch, err := srv.buildPipeline(req.Provider, req.Model)
	if err != nil {
		jobCancel()
		http.Error(w, "failed to build pipeline: "+err.Error(), http.StatusInternalServerError)
		return
	}

	job := &Job{
		ID:        jobID,
		Status:    JobRunning,
		Prompt:    req.Prompt,
		Provider:  req.Provider,
		Model:     req.Model,
		OutputDir: req.OutputDir,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		SharedCtx: sharedCtx,
		Orch:      orch,
		events:    eventCh,
		ctx:       jobCtx,
		cancel:    jobCancel,
	}
	srv.store.add(job)

	// Run pipeline in background goroutine.
	go func() {
		defer jobCancel()
		defer close(eventCh)

		if err := orch.Run(jobCtx, sharedCtx); err != nil {
			job.Status = JobFailed
			job.Error = err.Error()
			job.UpdatedAt = time.Now()
			return
		}

		// Persist files to disk.
		writer := output.New(req.OutputDir, true)
		if err := writer.WriteAll(sharedCtx); err != nil {
			job.Status = JobFailed
			job.Error = err.Error()
			job.UpdatedAt = time.Now()
			return
		}
		_ = writer.SaveContext(sharedCtx)

		job.Status = JobComplete
		job.UpdatedAt = time.Now()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

// handleSSE streams pipeline events for a job via Server-Sent Events.
func (srv *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/events/")
	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Convert the request context done channel to a plain chan struct{}.
	reqDone := make(chan struct{})
	go func() {
		<-r.Context().Done()
		close(reqDone)
	}()

	streamEvents(sse, job, reqDone)
}

// handleGetJob returns the full job state as JSON.
func (srv *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	// Strip any trailing path like /files or /files/...
	if idx := strings.Index(jobID, "/"); idx != -1 {
		jobID = jobID[:idx]
	}

	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	type jobResponse struct {
		ID        string    `json:"id"`
		Status    JobStatus `json:"status"`
		Prompt    string    `json:"prompt"`
		Provider  string    `json:"provider"`
		Model     string    `json:"model"`
		OutputDir string    `json:"output_dir"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Error     string    `json:"error,omitempty"`
		FileCount int       `json:"file_count"`
	}

	resp := jobResponse{
		ID:        job.ID,
		Status:    job.Status,
		Prompt:    job.Prompt,
		Provider:  job.Provider,
		Model:     job.Model,
		OutputDir: job.OutputDir,
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
		Error:     job.Error,
	}
	if job.SharedCtx != nil {
		resp.FileCount = len(job.SharedCtx.GetAllFiles())
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleListFiles returns the list of generated file paths.
func (srv *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	// Path: /api/jobs/{id}/files
	rest := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	parts := strings.SplitN(rest, "/", 2)
	jobID := parts[0]

	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	if job.SharedCtx == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]fileListEntry{})
		return
	}

	files := job.SharedCtx.GetAllFiles()
	entries := make([]fileListEntry, 0, len(files))
	for path, content := range files {
		entries = append(entries, fileListEntry{Path: path, Size: len(content)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

// handleGetFile returns the content of a single generated file.
func (srv *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	// Path: /api/jobs/{id}/files/{filePath}
	rest := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	// rest = "{id}/files/{filePath}"
	parts := strings.SplitN(rest, "/files/", 2)
	if len(parts) != 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	jobID := parts[0]
	filePath := parts[1]

	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	content, found := job.SharedCtx.GetFile(filePath)
	if !found {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

// handleIterate triggers an iteration run on an existing job.
func (srv *Server) handleIterate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /api/jobs/{id}/iterate
	rest := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	parts := strings.SplitN(rest, "/", 2)
	jobID := parts[0]

	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	if job.Status == JobRunning || job.Status == JobIterating {
		http.Error(w, "job is already running", http.StatusConflict)
		return
	}

	var req IterateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Feedback) == "" {
		http.Error(w, "feedback is required", http.StatusBadRequest)
		return
	}

	// Re-open the event channel.
	eventCh := make(chan vibecontext.Event, eventBufSize)
	iterCtx, iterCancel := context.WithCancel(context.Background())
	job.events = eventCh
	job.ctx = iterCtx
	job.cancel = iterCancel
	job.Status = JobIterating
	job.UpdatedAt = time.Now()
	job.SharedCtx.EventBus = eventCh

	go func() {
		defer iterCancel()
		defer close(eventCh)

		if err := job.Orch.Iterate(iterCtx, job.SharedCtx, req.Feedback); err != nil {
			job.Status = JobFailed
			job.Error = err.Error()
			job.UpdatedAt = time.Now()
			return
		}

		writer := output.New(job.OutputDir, true)
		if err := writer.WriteAll(job.SharedCtx); err != nil {
			job.Status = JobFailed
			job.Error = err.Error()
			job.UpdatedAt = time.Now()
			return
		}
		_ = writer.SaveContext(job.SharedCtx)

		job.Status = JobComplete
		job.UpdatedAt = time.Now()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

// handleGetContext returns the full SharedContext JSON for a job.
func (srv *Server) handleGetContext(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if idx := strings.Index(jobID, "/"); idx != -1 {
		jobID = jobID[:idx]
	}

	job, ok := srv.store.get(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	data, err := job.SharedCtx.Serialize()
	if err != nil {
		http.Error(w, "serialize error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
