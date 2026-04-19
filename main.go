package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"

	"atlas.windowcontroller/window"
)

//go:embed templates/*
var embeddedFS embed.FS

func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	if len(ips) == 0 {
		return []string{"127.0.0.1"}
	}
	return ips
}

var Version = "dev" // Overwritten by gobake build

func main() {
	// Basic flag handling
	if len(os.Args) > 1 {
		if os.Args[1] == "-v" || os.Args[1] == "--version" {
			fmt.Printf("atlas.windowcontroller v%s\n", Version)
			return
		}
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("Atlas Window Controller")
			fmt.Println("Usage: atlas.windowcontroller [flags]")
			fmt.Println("\nFlags:")
			fmt.Println("  -v, --version  Show version")
			fmt.Println("  -h, --help     Show help")
			return
		}
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/windows", handleListWindows)
	http.HandleFunc("/api/focus", handleFocusWindow)
	http.HandleFunc("/api/snap", handleSnapWindow)
	http.HandleFunc("/api/open", handleOpenApp)
	http.HandleFunc("/api/close", handleCloseWindow)
	http.HandleFunc("/api/volume", handleSetVolume)
	http.HandleFunc("/api/shutdown", handleShutdown)
	http.HandleFunc("/api/type", handleTypeString)
	http.HandleFunc("/api/key", handleSendKey)
	http.HandleFunc("/api/clipboard/get", handleGetClipboard)
	http.HandleFunc("/api/clipboard/set", handleSetClipboard)
	http.HandleFunc("/api/screenshot", handleScreenshot)
	http.HandleFunc("/api/paste", handlePaste)
	http.HandleFunc("/api/maximize", handleMaximizeWindow)
	http.HandleFunc("/api/minimize", handleMinimizeWindow)

	// Sub-directory the embedded filesystem to the "templates" folder
	templatesFS, err := fs.Sub(embeddedFS, "templates")
	if err != nil {
		fmt.Printf("Error sub-directorying embedded FS: %v\n", err)
		os.Exit(1)
	}

	// Serve static files from the sub-FS
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(templatesFS))))

	port := "5000"
	ips := getLocalIPs()
	fmt.Printf("Server starting at:\n")
	for _, ip := range ips {
		fmt.Printf("  http://%s:%s\n", ip, port)
	}
	fmt.Printf("\nControl this PC over WiFi using any of the URLs above.\n")

	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Parse templates from the full embeddedFS using the "templates/" prefix
	tmpl, err := template.ParseFS(embeddedFS, "templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := struct {
		IPs []string
	}{
		IPs: getLocalIPs(),
	}
	tmpl.Execute(w, data)
}

func handleListWindows(w http.ResponseWriter, r *http.Request) {
	windows, err := window.ListWindows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(windows)
}

func handleFocusWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.FocusWindow(req.Handle); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleMaximizeWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.MaximizeWindow(req.Handle); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleMinimizeWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.MinimizeWindow(req.Handle); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleSnapWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle   string `json:"handle"`
		Position string `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.SnapWindow(req.Handle, req.Position); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleOpenApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.OpenApp(req.Command); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleCloseWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.CloseWindow(req.Handle); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleSetVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.SetVolume(req.Level); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := window.ShutdownPC(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleTypeString(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.TypeString(req.Handle, req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleSendKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
		Key    string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.SendKey(req.Handle, req.Key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleGetClipboard(w http.ResponseWriter, r *http.Request) {
	text, err := window.GetClipboard()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Text string `json:"text"`
	}{Text: text})
}

func handleSetClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.SetClipboard(req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleScreenshot(w http.ResponseWriter, r *http.Request) {
	handle := r.URL.Query().Get("handle")
	if handle == "" {
		http.Error(w, "Handle is required", http.StatusBadRequest)
		return
	}

	img, err := window.CaptureWindow(handle)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(img)
}

func handlePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := window.PasteIntoWindow(req.Handle); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
