package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Gateway struct {
	cfg       *Config
	providers map[string]Provider
	tasks     *TaskStore
	events    *EventBroker
	mu        sync.Mutex
	cancels   map[string]context.CancelFunc
}

func NewGateway(cfg *Config) *Gateway {
	g := &Gateway{
		cfg:       cfg,
		providers: map[string]Provider{},
		tasks:     NewTaskStore(cfg.DataDir),
		events:    NewEventBroker(),
		cancels:   make(map[string]context.CancelFunc),
	}
	for name, pc := range cfg.Providers {
		g.providers[name] = newProvider(pc)
	}
	return g
}

func (g *Gateway) modelsHandler(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID             string `json:"id"`
		UseVision      bool   `json:"use_vision"`
		UseToolCalling bool   `json:"use_tool_calling"`
	}
	list := make([]modelInfo, 0, len(g.cfg.Models))
	for name, entry := range g.cfg.Models {
		list = append(list, modelInfo{ID: name, UseVision: entry.UseVision, UseToolCalling: entry.UseToolCalling})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
}

func (g *Gateway) chatHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var meta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	mc, err := g.GetModel(meta.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	p, ok := g.providers[mc.Provider]
	if !ok {
		writeError(w, http.StatusBadGateway, "provider not found")
		return
	}

	body, err = replaceModelInBody(body, mc.UpstreamModel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rewrite model")
		return
	}

	resp, err := p.Complete(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if meta.Stream {
		streamCopy(w, resp.Body)
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

func (g *Gateway) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	tasks, err := g.tasks.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (g *Gateway) createTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" || req.Description == "" || req.Model == "" {
		writeError(w, http.StatusBadRequest, "url, description, and model are required")
		return
	}
	if req.Name == "" {
		req.Name = "Untitled Task"
	}

	task := &Task{
		ID:          newID(),
		Name:        req.Name,
		URL:         req.URL,
		Description: req.Description,
		Model:       req.Model,
		Status:      "pending",
		Steps:       []Step{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := g.tasks.Save(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (g *Gateway) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := g.tasks.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (g *Gateway) updateTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := g.tasks.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	g.mu.Lock()
	_, running := g.cancels[id]
	g.mu.Unlock()
	if running {
		writeError(w, http.StatusConflict, "cannot update a running task")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		URL         *string `json:"url"`
		Description *string `json:"description"`
		Model       *string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.URL != nil {
		task.URL = *req.URL
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Model != nil {
		task.Model = *req.Model
	}
	task.UpdatedAt = time.Now()
	if err := g.tasks.Save(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (g *Gateway) deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g.mu.Lock()
	if cancel, ok := g.cancels[id]; ok {
		cancel()
		delete(g.cancels, id)
	}
	g.mu.Unlock()
	g.tasks.Delete(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (g *Gateway) runTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := g.tasks.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	g.mu.Lock()
	_, actuallyRunning := g.cancels[id]
	g.mu.Unlock()
	if task.Status == "running" && actuallyRunning {
		writeError(w, http.StatusConflict, "task is already running")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	g.mu.Lock()
	g.cancels[id] = cancel
	g.mu.Unlock()

	go func() {
		defer func() {
			g.mu.Lock()
			delete(g.cancels, id)
			g.mu.Unlock()
			cancel()
		}()
		g.runAgent(ctx, task)
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (g *Gateway) stopTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g.mu.Lock()
	cancel, ok := g.cancels[id]
	if ok {
		cancel()
		delete(g.cancels, id)
	}
	g.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "task is not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (g *Gateway) taskEventsHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := g.events.Subscribe(id)
	defer g.events.Unsubscribe(id, ch)

	task, err := g.tasks.Get(id)
	if err == nil {
		data, _ := json.Marshal(task)
		fmt.Fprintf(w, "event: init\ndata: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, evt.JSON())
			flusher.Flush()
		}
	}
}

func replaceModelInBody(body []byte, newModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	modelBytes, err := json.Marshal(newModel)
	if err != nil {
		return nil, err
	}
	raw["model"] = modelBytes
	return json.Marshal(raw)
}

func streamCopy(w http.ResponseWriter, r io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailers":            true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		if hopByHopHeaders[k] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg}})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/screenshots/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

func main() {
	cfgPath := "config.json"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	g := NewGateway(cfg)

	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dashboard.html")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// LLM Gateway
	mux.HandleFunc("GET /v1/models", g.modelsHandler)
	mux.HandleFunc("POST /v1/chat/completions", g.chatHandler)

	// Task API
	mux.HandleFunc("GET /api/tasks", g.listTasksHandler)
	mux.HandleFunc("POST /api/tasks", g.createTaskHandler)
	mux.HandleFunc("GET /api/tasks/{id}", g.getTaskHandler)
	mux.HandleFunc("PATCH /api/tasks/{id}", g.updateTaskHandler)
	mux.HandleFunc("DELETE /api/tasks/{id}", g.deleteTaskHandler)
	mux.HandleFunc("POST /api/tasks/{id}/run", g.runTaskHandler)
	mux.HandleFunc("POST /api/tasks/{id}/stop", g.stopTaskHandler)
	mux.HandleFunc("GET /api/tasks/{id}/events", g.taskEventsHandler)

	// Screenshots
	screenshotFS := http.StripPrefix("/screenshots/", http.FileServer(http.Dir(g.tasks.ScreenshotDir())))
	mux.Handle("GET /screenshots/", screenshotFS)

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: loggingMiddleware(mux),
	}

	go func() {
		log.Printf("LLMTestPilot listening on %s", cfg.Listen)
		log.Printf("Dashboard: http://localhost%s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}
