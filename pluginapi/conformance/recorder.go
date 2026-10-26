package conformance

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const (
	listenAddrDefault = "127.0.0.1:0"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

type recorder struct {
	baseURL string

	mu       sync.Mutex
	results  map[string][]pluginapi.Result
	progress map[string][]pluginapi.Progress
}

func startRecorder(t *testing.T, listenAddr, baseURL string) *recorder {
	t.Helper()
	if listenAddr == "" {
		listenAddr = listenAddrDefault
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		t.Fatalf("listen for callbacks on %s: %v", listenAddr, err)
	}
	if baseURL == "" {
		baseURL = "http://" + listener.Addr().String()
	}
	rec := &recorder{
		baseURL:  baseURL,
		results:  map[string][]pluginapi.Result{},
		progress: map[string][]pluginapi.Progress{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs/{job_id}/result", rec.handleResult)
	mux.HandleFunc("POST /jobs/{job_id}/progress", rec.handleProgress)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), shutdownTimeout)
		defer cancel()
		err := srv.Shutdown(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			err = srv.Close()
		}
		if err != nil {
			t.Errorf("shutdown callback listener: %v", err)
		}
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("callback listener: %v", err)
		}
	})
	return rec
}

func (r *recorder) handleResult(w http.ResponseWriter, req *http.Request) {
	var res pluginapi.Result
	if err := json.NewDecoder(req.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := req.PathValue("job_id")
	r.mu.Lock()
	r.results[id] = append(r.results[id], res)
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (r *recorder) handleProgress(w http.ResponseWriter, req *http.Request) {
	var p pluginapi.Progress
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := req.PathValue("job_id")
	r.mu.Lock()
	r.progress[id] = append(r.progress[id], p)
	r.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (r *recorder) callback(jobID string) pluginapi.Callback {
	return pluginapi.Callback{
		ResultURL:   r.baseURL + "/jobs/" + jobID + "/result",
		ProgressURL: r.baseURL + "/jobs/" + jobID + "/progress",
	}
}

func (r *recorder) resultsFor(jobID string) []pluginapi.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pluginapi.Result(nil), r.results[jobID]...)
}

func (r *recorder) progressFor(jobID string) []pluginapi.Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pluginapi.Progress(nil), r.progress[jobID]...)
}

func newUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
