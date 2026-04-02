package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	vibecontext "github.com/dhawalhost/vibe-agents/internal/context"
)

// sseWriter wraps an http.ResponseWriter to write SSE frames.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, flusher: flusher}, true
}

func (s *sseWriter) send(eventType string, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, encoded)
	s.flusher.Flush()
	return err
}

func (s *sseWriter) sendRaw(eventType, data string) {
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data)
	s.flusher.Flush()
}

func (s *sseWriter) sendKeepAlive() {
	fmt.Fprintf(s.w, ": keep-alive\n\n")
	s.flusher.Flush()
}

// streamEvents fans-out events from a job's EventBus to an SSE connection.
// It returns when the pipeline_complete or error event arrives, the job ctx
// is cancelled, or the HTTP client disconnects.
func streamEvents(sse *sseWriter, job *Job, reqCtx <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt, ok := <-job.events:
			if !ok {
				// channel closed — pipeline finished
				return
			}
			_ = sse.send(evt.Type, evt)
			if evt.Type == "pipeline_complete" || evt.Type == "error" {
				return
			}
		case <-ticker.C:
			sse.sendKeepAlive()
		case <-job.ctx.Done():
			_ = sse.send("error", vibecontext.Event{Type: "error", Message: "pipeline cancelled"})
			return
		case <-reqCtx:
			return
		}
	}
}
