package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"wispwind/internal/db"
	"wispwind/internal/usage"
)

//go:embed web
var webFS embed.FS

type Server struct {
	db         *db.DB
	logsDir    string
	historyDir string
	indexHTML  []byte
	staticFS   http.Handler
	onReload   func()
}

func Start(database *db.DB, logsDir, historyDir string, onReload func()) (string, error) {
	indexBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return "", fmt.Errorf("read embedded index: %w", err)
	}
	staticSub, err := fs.Sub(webFS, "web/static")
	if err != nil {
		return "", fmt.Errorf("sub static fs: %w", err)
	}

	s := &Server{
		db:         database,
		logsDir:    logsDir,
		historyDir: historyDir,
		indexHTML:  indexBytes,
		staticFS:   http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))),
		onReload:   onReload,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", s.staticFS)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/usage/today", s.handleTodayUsage)
	mux.HandleFunc("/api/logs", s.handleListDir(logsDir))
	mux.HandleFunc("/api/history/dates", s.handleHistoryDates)
	mux.HandleFunc("/api/history/by-date", s.handleHistoryByDate)

	// Serve the actual files
	mux.Handle("/logs/", http.StripPrefix("/logs/", http.FileServer(http.Dir(logsDir))))
	mux.Handle("/history/", http.StripPrefix("/history/", http.FileServer(http.Dir(historyDir))))

	preferredPort := 8182
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	go http.Serve(listener, mux)
	return url, nil
}

func (s *Server) handleListDir(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries, err := os.ReadDir(dir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}
		json.NewEncoder(w).Encode(files)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(s.indexHTML)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" {
		settings, err := s.db.GetAllSettings(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(settings)
		return
	}

	if r.Method == "POST" {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		for k, v := range updates {
			if err := s.db.SaveSetting(r.Context(), k, v); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if s.onReload != nil {
			s.onReload()
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleTodayUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	records, err := s.db.GetTodayUsage(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if records == nil {
		records = []usage.Record{} // Return empty array instead of null
	}
	json.NewEncoder(w).Encode(records)
}

func (s *Server) handleHistoryDates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dates, err := s.db.GetHistoryDates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dates == nil {
		dates = []string{}
	}
	json.NewEncoder(w).Encode(dates)
}

func (s *Server) handleHistoryByDate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dateStr := r.URL.Query().Get("date")
	records, err := s.db.GetHistoryByDate(r.Context(), dateStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if records == nil {
		records = []usage.Record{}
	}
	json.NewEncoder(w).Encode(records)
}
