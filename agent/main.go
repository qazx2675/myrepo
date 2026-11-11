package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"host-cross-checker/internal/launcher"
	"host-cross-checker/internal/webui"
)

// Config represents the agent configuration.
type Config struct {
	SharedDir                string `json:"sharedDir"`
	ListenPort               int    `json:"listenPort"`
	PollIntervalMs           int    `json:"pollIntervalMs"`
	AutoOpenBrowserOnRequest bool   `json:"autoOpenBrowserOnRequest"`
	BindIP                   string `json:"bindIp,omitempty"`
	AwxBaseURL               string `json:"awxBaseUrl,omitempty"`
	AwxUsername              string `json:"awxUsername,omitempty"`
	AwxPassword              string `json:"awxPassword,omitempty"`
	Headless                 bool   `json:"headless,omitempty"`
}

// User represents a system user.
type User struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Role string `json:"role,omitempty"`
}

// UsersFile represents the users.json structure.
type UsersFile struct {
	UpdatedAt string `json:"updatedAt"`
	Users     []User `json:"users"`
}

// Metadata for a session.
type Metadata struct {
	IncidentId      string `json:"incidentId"`
	SpecialNotes    string `json:"specialNotes"`
	AwxContextLabel string `json:"awxContextLabel"`
	ExcludePattern  string `json:"excludePattern"`
}

// ActiveFields indicates which fields are checked.
type ActiveFields struct {
	Awx      bool `json:"awx"`
	Detail   bool `json:"detail"`
	Incident bool `json:"incident"`
	Target   bool `json:"target"`
}

// WorkerData holds the host lists collected by the worker.
type WorkerData struct {
	AwxHosts      []string `json:"awxHosts"`
	ExcludedHosts []string `json:"excludedHosts"`
	RawAwxCount   int      `json:"rawAwxCount"`
	DetailHosts   []string `json:"detailHosts"`
	IncidentHosts []string `json:"incidentHosts"`
	TargetHosts   []string `json:"targetHosts"`
}

// CheckerData holds the checker's verification data and decision.
type CheckerData struct {
	DetailHosts     []string `json:"detailHosts"`
	IncidentHosts   []string `json:"incidentHosts"`
	TargetHosts     []string `json:"targetHosts"`
	IsAllMatch      bool     `json:"isAllMatch"`
	Decision        string   `json:"decision"` // "APPROVED" or "REJECTED"
	DecisionComment string   `json:"decisionComment"`
}

// CheckerHistoryItem records a reassignment event.
type CheckerHistoryItem struct {
	Checker      User   `json:"checker"`
	ReassignedAt string `json:"reassignedAt"`
}

// Session represents a verification session.
type Session struct {
	SessionId      string               `json:"sessionId"`
	Status         string               `json:"status"` // "PENDING", "CHECKING", "APPROVED", "REJECTED"
	CreatedAt      string               `json:"createdAt"`
	DecidedAt      string               `json:"decidedAt,omitempty"`
	Worker         User                 `json:"worker"`
	Checker        User                 `json:"checker"`
	Metadata       Metadata             `json:"metadata"`
	ActiveFields   ActiveFields         `json:"activeFields"`
	WorkerData     WorkerData           `json:"workerData"`
	CheckerData    *CheckerData         `json:"checkerData,omitempty"`
	CheckerHistory []CheckerHistoryItem `json:"checkerHistory,omitempty"`
}

// HostPreview holds truncated preview info for blinding.
type HostPreview struct {
	PreviewHosts []string `json:"previewHosts"`
	TotalCount   int      `json:"totalCount"`
	BlindedCount int      `json:"blindedCount"`
}

// SSEEvent represents an event sent to SSE clients.
type SSEEvent struct {
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// SSEBroker coordinates SSE client connections and event broadcasting.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[chan SSEEvent]bool
}

func newSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan SSEEvent]bool),
	}
}

func (b *SSEBroker) AddClient(ch chan SSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[ch] = true
}

func (b *SSEBroker) RemoveClient(ch chan SSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, ch)
}

func (b *SSEBroker) Broadcast(eventType, sessionId string, data interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	evt := SSEEvent{
		Type:      eventType,
		SessionId: sessionId,
		Data:      data,
	}
	for ch := range b.clients {
		select {
		case ch <- evt:
		default:
			// Non-blocking drop if client is saturated
		}
	}
}

// WSClient represents an active WebSocket client connection.
type WSClient struct {
	conn net.Conn
	mu   sync.Mutex
}

func (c *WSClient) WriteText(payload []byte) error {
	length := len(payload)
	var header []byte
	if length < 126 {
		header = []byte{0x81, byte(length)}
	} else if length < 65536 {
		header = make([]byte, 4)
		header[0] = 0x81
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
	} else {
		header = make([]byte, 10)
		header[0] = 0x81
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:10], uint64(length))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if length > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) WritePong(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	header := []byte{0x8A, byte(len(payload))}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := c.conn.Write(payload)
		return err
	}
	return nil
}

// WSHub coordinates active WebSocket connections and broadcasting.
type WSHub struct {
	mu      sync.RWMutex
	clients map[*WSClient]bool
}

func newWSHub() *WSHub {
	return &WSHub{clients: make(map[*WSClient]bool)}
}

func (h *WSHub) Add(c *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
}

func (h *WSHub) Remove(c *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (h *WSHub) BroadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		_ = c.WriteText(data)
	}
}

func wsUpgrade(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, fmt.Errorf("missing Sec-WebSocket-Key")
	}
	hash := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(hash[:])

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return nil, fmt.Errorf("hijack not supported")
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	res := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := bufrw.WriteString(res); err != nil {
		conn.Close()
		return nil, err
	}
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func wsReadMessage(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0F
	isMasked := (header[1] & 0x80) != 0
	payloadLen := int(header[1] & 0x7F)

	if payloadLen == 126 {
		lenBytes := make([]byte, 2)
		if _, err := io.ReadFull(r, lenBytes); err != nil {
			return 0, nil, err
		}
		payloadLen = int(binary.BigEndian.Uint16(lenBytes))
	} else if payloadLen == 127 {
		lenBytes := make([]byte, 8)
		if _, err := io.ReadFull(r, lenBytes); err != nil {
			return 0, nil, err
		}
		payloadLen = int(binary.BigEndian.Uint64(lenBytes))
	}

	var mask [4]byte
	if isMasked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}

	if isMasked {
		for i := 0; i < payloadLen; i++ {
			payload[i] ^= mask[i%4]
		}
	}

	return opcode, payload, nil
}

type wsResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newWSResponseRecorder() *wsResponseRecorder {
	return &wsResponseRecorder{header: make(http.Header), code: 200}
}
func (r *wsResponseRecorder) Header() http.Header         { return r.header }
func (r *wsResponseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *wsResponseRecorder) WriteHeader(statusCode int)  { r.code = statusCode }

// WSRequestMsg represents an incoming request over WebSocket.
type WSRequestMsg struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// WSResponseMsg represents a response back over WebSocket.
type WSResponseMsg struct {
	ID     string      `json:"id"`
	Status int         `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// awxCacheEntry stores cached inventory hosts from AWX.
type awxCacheEntry struct {
	hosts     []string
	fetchedAt time.Time
}

// AgentServer represents the main HTTP backend service.
type AgentServer struct {
	config        Config
	usersMutex    sync.RWMutex
	sessMutex     sync.RWMutex
	sseBroker     *SSEBroker
	wsHub         *WSHub
	httpClient    *http.Client
	awxHttpClient *http.Client
	awxCacheMu    sync.RWMutex
	awxCache      map[string]awxCacheEntry
	stopChan      chan struct{}
	fileModTime   map[string]time.Time
	modMu         sync.Mutex
	detectedIP    string
	lifecycleMu   sync.Mutex
	hasSeenClient bool
	lastPingTime  time.Time
	byeReceived   bool
	byeTime       time.Time
	onShutdown    func()
}

// broadcastEvent sends events to both SSE clients and WebSocket clients.
func (s *AgentServer) broadcastEvent(eventType, sessionId string, data interface{}) {
	s.sseBroker.Broadcast(eventType, sessionId, data)
	if s.wsHub != nil {
		s.wsHub.BroadcastJSON(map[string]interface{}{
			"event":     eventType,
			"sessionId": sessionId,
			"data":      data,
		})
	}
}

// NewAgentServer creates and initializes an AgentServer.
func NewAgentServer(cfg Config) (*AgentServer, error) {
	if cfg.SharedDir == "" {
		cfg.SharedDir = "."
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 18080
	}
	if cfg.PollIntervalMs == 0 {
		cfg.PollIntervalMs = 500
	}
	if cfg.AwxBaseURL == "" {
		if envURL := os.Getenv("AWX_BASE_URL"); envURL != "" {
			cfg.AwxBaseURL = strings.TrimRight(envURL, "/")
		} else {
			cfg.AwxBaseURL = "http://192.168.0.58:30080"
		}
	}
	if cfg.AwxUsername == "" {
		if envUser := os.Getenv("AWX_USERNAME"); envUser != "" {
			cfg.AwxUsername = envUser
		} else {
			cfg.AwxUsername = "test.kim"
		}
	}
	if cfg.AwxPassword == "" {
		if envPass := os.Getenv("AWX_PASSWORD"); envPass != "" {
			cfg.AwxPassword = envPass
		} else {
			cfg.AwxPassword = "11111111!"
		}
	}

	sessionsDir := filepath.Join(cfg.SharedDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	detectedIP := detectLocalIP(cfg.BindIP)

	s := &AgentServer{
		config:        cfg,
		sseBroker:     newSSEBroker(),
		wsHub:         newWSHub(),
		httpClient:    &http.Client{Timeout: 1 * time.Second},
		awxHttpClient: &http.Client{Timeout: 8 * time.Second},
		awxCache:      make(map[string]awxCacheEntry),
		stopChan:      make(chan struct{}),
		fileModTime:   make(map[string]time.Time),
		detectedIP:    detectedIP,
	}

	if err := s.ensureUsersFile(); err != nil {
		return nil, fmt.Errorf("failed to ensure users.json: %w", err)
	}

	return s, nil
}

// detectLocalIP determines the host IP address.
func detectLocalIP(bindIP string) string {
	if bindIP != "" {
		return bindIP
	}
	// Try outbound UDP connection to determine primary interface
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}
	// Fallback to iterating network interfaces
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					return ipNet.IP.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// ensureUsersFile creates or verifies users.json in sharedDir.
func (s *AgentServer) ensureUsersFile() error {
	s.usersMutex.Lock()
	defer s.usersMutex.Unlock()

	usersPath := filepath.Join(s.config.SharedDir, "users.json")
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		// Check fallback from "config/users.json"
		fallbackPath := filepath.Join("config", "users.json")
		if data, err := os.ReadFile(fallbackPath); err == nil {
			if err := os.WriteFile(usersPath, data, 0644); err == nil {
				return nil
			}
		}

		defaultUsers := UsersFile{
			UpdatedAt: time.Now().Format(time.RFC3339),
			Users: []User{
				{Name: "김철수", IP: "192.168.0.23", Role: "작업자"},
				{Name: "이영희", IP: "192.168.0.102", Role: "확인자"},
				{Name: "박민수", IP: "192.168.0.105", Role: "확인자"},
				{Name: "정관리", IP: "192.168.0.58", Role: "엔지니어"},
			},
		}
		data, err := json.MarshalIndent(defaultUsers, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(usersPath, data, 0644)
	}
	return nil
}

// readUsers reads users.json thread-safely.
func (s *AgentServer) readUsers() (UsersFile, error) {
	s.usersMutex.RLock()
	defer s.usersMutex.RUnlock()

	usersPath := filepath.Join(s.config.SharedDir, "users.json")
	data, err := os.ReadFile(usersPath)
	if err != nil {
		return UsersFile{}, err
	}
	var uf UsersFile
	if err := json.Unmarshal(data, &uf); err != nil {
		return UsersFile{}, err
	}
	return uf, nil
}

// writeUsers writes users.json thread-safely.
func (s *AgentServer) writeUsers(uf UsersFile) error {
	s.usersMutex.Lock()
	defer s.usersMutex.Unlock()

	usersPath := filepath.Join(s.config.SharedDir, "users.json")
	uf.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(uf, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write
	tmpPath := fmt.Sprintf("%s.tmp.%d", usersPath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, usersPath); err != nil {
		_ = os.Remove(tmpPath)
		return os.WriteFile(usersPath, data, 0644)
	}
	return nil
}

// sessionFilePath returns the file path for a session.
func (s *AgentServer) sessionFilePath(sessionId string) string {
	cleanId := filepath.Base(sessionId)
	return filepath.Join(s.config.SharedDir, "sessions", cleanId+".json")
}

// saveSession writes a session file atomically.
func (s *AgentServer) saveSession(sess *Session) error {
	s.sessMutex.Lock()
	defer s.sessMutex.Unlock()

	filePath := s.sessionFilePath(sess.SessionId)
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return os.WriteFile(filePath, data, 0644)
	}
	return nil
}

// loadSession reads a session from file thread-safely.
func (s *AgentServer) loadSession(sessionId string) (*Session, error) {
	s.sessMutex.RLock()
	defer s.sessMutex.RUnlock()

	filePath := s.sessionFilePath(sessionId)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// StartBackgroundPolling starts the session directory watcher.
func (s *AgentServer) StartBackgroundPolling() {
	interval := time.Duration(s.config.PollIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.stopChan:
				return
			case <-ticker.C:
				s.pollSessionsDir()
			}
		}
	}()
}

// Stop stops the server background routines.
func (s *AgentServer) Stop() {
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

// postPeerJSON sends an HTTP POST request to a peer, properly draining and closing response body.
func (s *AgentServer) postPeerJSON(url string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err == nil && resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// pollSessionsDir checks for updated session files on disk.
func (s *AgentServer) pollSessionsDir() {
	pattern := filepath.Join(s.config.SharedDir, "sessions", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	// Limit check to the most recent 100 files to prevent unbounded disk/SMB stat overhead
	if len(files) > 100 {
		files = files[len(files)-100:]
	}

	// Periodic pruning of in-memory fileModTime map to prevent memory leak
	s.modMu.Lock()
	if len(s.fileModTime) > 1000 {
		activeSet := make(map[string]struct{}, len(files))
		for _, f := range files {
			activeSet[f] = struct{}{}
		}
		for k := range s.fileModTime {
			if _, ok := activeSet[k]; !ok {
				delete(s.fileModTime, k)
			}
		}
	}
	s.modMu.Unlock()

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		s.modMu.Lock()
		prevTime, exists := s.fileModTime[file]
		modTime := info.ModTime()
		if !exists {
			s.fileModTime[file] = modTime
			s.modMu.Unlock()
			continue // Initial detection, do not blast duplicate SSE
		}

		if modTime.After(prevTime) {
			s.fileModTime[file] = modTime
			s.modMu.Unlock()

			// File updated!
			data, err := os.ReadFile(file)
			if err == nil {
				var sess Session
				if err := json.Unmarshal(data, &sess); err == nil {
					s.broadcastEvent("SESSION_UPDATED", sess.SessionId, sess)
				}
			}
		} else {
			s.modMu.Unlock()
		}
	}
}

// generateSessionID generates a unique ID formatted as req_YYYYMMDD_HHMMSS_<rand>.
func generateSessionID() string {
	now := time.Now().Format("20060102_150405")
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req_%s_%04x", now, time.Now().UnixNano()%0x10000)
	}
	return fmt.Sprintf("req_%s_%s", now, hex.EncodeToString(b))
}

// getClientIP extracts the client's IP from query, headers, or remote address.
func getClientIP(r *http.Request) string {
	if ip := r.URL.Query().Get("clientIp"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Client-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("clientIp"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// ipMatches compares two IP strings with loopback tolerance.
func ipMatches(ip1, ip2 string) bool {
	if ip1 == ip2 {
		return true
	}
	isLoopback1 := ip1 == "127.0.0.1" || ip1 == "::1" || ip1 == "localhost"
	isLoopback2 := ip2 == "127.0.0.1" || ip2 == "::1" || ip2 == "localhost"
	return isLoopback1 && isLoopback2
}

// makeHostPreview creates a 3-item preview with total and blinded counts.
func makeHostPreview(hosts []string) HostPreview {
	total := len(hosts)
	pLen := 3
	if total < pLen {
		pLen = total
	}
	preview := make([]string, pLen)
	copy(preview, hosts[:pLen])
	return HostPreview{
		PreviewHosts: preview,
		TotalCount:   total,
		BlindedCount: total - pLen,
	}
}

// enableCORS sets CORS headers and handles preflight OPTIONS.
func enableCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

// writeJSON writes a JSON response with status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// Routes sets up the HTTP handlers for the agent server.
func (s *AgentServer) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleAppRoot)
	mux.HandleFunc("/awx_host_checker.js", webui.ServeCheckerJS)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/ping", s.handlePing)
	mux.HandleFunc("/api/bye", s.handleBye)
	mux.HandleFunc("/api/awx/hosts", s.handleAwxHosts)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/users", s.handleUsers)
	mux.HandleFunc("/api/users/register", s.handleRegister)
	mux.HandleFunc("/api/session/create", s.handleSessionCreate)
	mux.HandleFunc("/api/session/status", s.handleSessionStatus)
	mux.HandleFunc("/api/session/list", s.handleSessionList)
	mux.HandleFunc("/api/session/reassign", s.handleSessionReassign)
	mux.HandleFunc("/api/session/decision", s.handleSessionDecision)
	mux.HandleFunc("/api/peer/notify", s.handlePeerNotify)
	mux.HandleFunc("/api/peer/update", s.handlePeerUpdate)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/checker", s.handleCheckerUI)
	mux.HandleFunc("/ws", s.handleWebSocket)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if enableCORS(w, r) {
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// handleAppRoot serves the standalone desktop application UI on GET /.
func (s *AgentServer) handleAppRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	s.recordClientPing()
	webui.ServeIndex(w, r)
}

// handleHealth: GET /api/health
func (s *AgentServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"port":   s.config.ListenPort,
		"ip":     s.detectedIP,
	})
}

func (s *AgentServer) recordClientPing() {
	s.lifecycleMu.Lock()
	s.hasSeenClient = true
	s.lastPingTime = time.Now()
	s.byeReceived = false
	s.lifecycleMu.Unlock()
}

// handlePing: POST/GET /api/ping
func (s *AgentServer) handlePing(w http.ResponseWriter, r *http.Request) {
	s.recordClientPing()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

// handleBye: POST/GET /api/bye
func (s *AgentServer) handleBye(w http.ResponseWriter, r *http.Request) {
	s.lifecycleMu.Lock()
	s.byeReceived = true
	s.byeTime = time.Now()
	onShutdown := s.onShutdown
	s.lifecycleMu.Unlock()

	if r.URL.Query().Get("force") == "true" {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if onShutdown != nil {
				onShutdown()
			} else {
				os.Exit(0)
			}
		}()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

// StartLifecycleWatcher monitors standalone app window heartbeats and triggers
// clean shutdown within ~1.2 seconds when the user closes the window.
func (s *AgentServer) StartLifecycleWatcher(onShutdown func()) {
	s.lifecycleMu.Lock()
	s.onShutdown = onShutdown
	s.lifecycleMu.Unlock()

	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopChan:
				return
			case <-ticker.C:
				s.lifecycleMu.Lock()
				seen := s.hasSeenClient
				lastPing := s.lastPingTime
				bye := s.byeReceived
				byeAt := s.byeTime
				s.lifecycleMu.Unlock()

				if seen {
					// Case 1: Explicit /api/bye received on window unload and no reload ping arrived within 1.2s
					if bye && time.Since(byeAt) >= 1200*time.Millisecond {
						if onShutdown != nil {
							onShutdown()
						}
						return
					}
					// Case 2: Browser window closed abruptly without beacon (no ping for > 6s)
					if !bye && time.Since(lastPing) >= 6*time.Second {
						if onShutdown != nil {
							onShutdown()
						}
						return
					}
				} else {
					// Case 3: Initial connect timeout (no client connected within 60s)
					if time.Since(startTime) > 60*time.Second {
						if onShutdown != nil {
							onShutdown()
						}
						return
					}
				}
			}
		}
	}()
}

// awxPageResponse models AWX /api/v2/inventories/{id}/hosts/ JSON payload.
type awxPageResponse struct {
	Count   int     `json:"count"`
	Next    *string `json:"next"`
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

func (s *AgentServer) fetchAwxPage(pageURL string) (*awxPageResponse, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	if s.config.AwxUsername != "" {
		req.SetBasicAuth(s.config.AwxUsername, s.config.AwxPassword)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.awxHttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AWX API returned HTTP %d", resp.StatusCode)
	}
	var parsed awxPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// fetchAwxInventoryHosts retrieves and caches all hosts for an AWX inventory.
func (s *AgentServer) fetchAwxInventoryHosts(invID string, refresh bool) ([]string, error) {
	if invID == "" {
		invID = "3"
	}
	if !refresh {
		s.awxCacheMu.RLock()
		entry, ok := s.awxCache[invID]
		s.awxCacheMu.RUnlock()
		if ok && time.Since(entry.fetchedAt) < 60*time.Second && len(entry.hosts) > 0 {
			cp := make([]string, len(entry.hosts))
			copy(cp, entry.hosts)
			return cp, nil
		}
	}

	var basePath string
	if invID == "all" || invID == "0" {
		basePath = fmt.Sprintf("%s/api/v2/hosts/?page_size=200", s.config.AwxBaseURL)
	} else {
		basePath = fmt.Sprintf("%s/api/v2/inventories/%s/hosts/?page_size=200", s.config.AwxBaseURL, invID)
	}

	page1, err := s.fetchAwxPage(basePath + "&page=1")
	if err != nil {
		return nil, err
	}

	totalPages := (page1.Count + 199) / 200
	if totalPages < 1 {
		totalPages = 1
	}

	pageHosts := make([][]string, totalPages)
	for _, item := range page1.Results {
		h := strings.ToLower(strings.TrimSpace(item.Name))
		if h != "" {
			pageHosts[0] = append(pageHosts[0], h)
		}
	}

	if totalPages > 1 {
		var wg sync.WaitGroup
		var firstErr error
		var errMu sync.Mutex

		for p := 2; p <= totalPages; p++ {
			wg.Add(1)
			go func(pageNum int) {
				defer wg.Done()
				u := fmt.Sprintf("%s&page=%d", basePath, pageNum)
				pg, err := s.fetchAwxPage(u)
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
				var list []string
				for _, item := range pg.Results {
					h := strings.ToLower(strings.TrimSpace(item.Name))
					if h != "" {
						list = append(list, h)
					}
				}
				pageHosts[pageNum-1] = list
			}(p)
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
	}

	seen := make(map[string]struct{}, page1.Count)
	allHosts := make([]string, 0, page1.Count)
	for _, list := range pageHosts {
		for _, h := range list {
			if _, exists := seen[h]; !exists {
				seen[h] = struct{}{}
				allHosts = append(allHosts, h)
			}
		}
	}
	sort.Strings(allHosts)

	s.awxCacheMu.Lock()
	s.awxCache[invID] = awxCacheEntry{
		hosts:     allHosts,
		fetchedAt: time.Now(),
	}
	s.awxCacheMu.Unlock()

	cp := make([]string, len(allHosts))
	copy(cp, allHosts)
	return cp, nil
}

// WarmupAwxCache preloads the specified inventory into memory in the background.
func (s *AgentServer) WarmupAwxCache(invID string) {
	_, _ = s.fetchAwxInventoryHosts(invID, false)
}

// handleAwxHosts: GET /api/awx/hosts?inventory=3&exclude=...&limit=...
func (s *AgentServer) handleAwxHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	invID := strings.TrimSpace(r.URL.Query().Get("inventory"))
	if invID == "" {
		invID = "3"
	}
	refresh := r.URL.Query().Get("refresh") == "true"
	excludePattern := strings.TrimSpace(r.URL.Query().Get("exclude"))
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))

	rawHosts, err := s.fetchAwxInventoryHosts(invID, refresh)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("AWX 호스트 수집 실패: %v", err)})
		return
	}

	if limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit < len(rawHosts) {
			rawHosts = rawHosts[:limit]
		}
	}

	var filters []string
	if excludePattern != "" {
		for _, part := range strings.Split(excludePattern, "|") {
			f := strings.ToLower(strings.TrimSpace(part))
			if f != "" {
				filters = append(filters, f)
			}
		}
	}

	filteredHosts := make([]string, 0, len(rawHosts))
	excludedHosts := make([]string, 0)
	results := make([]map[string]string, 0, len(rawHosts))

	for _, h := range rawHosts {
		results = append(results, map[string]string{"name": h})
		excluded := false
		for _, f := range filters {
			if h == f || strings.Contains(h, f) {
				excluded = true
				break
			}
		}
		if excluded {
			excludedHosts = append(excludedHosts, h)
		} else {
			filteredHosts = append(filteredHosts, h)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":         len(filteredHosts),
		"rawCount":      len(rawHosts),
		"next":          nil,
		"inventoryId":   invID,
		"hosts":         filteredHosts,
		"excludedHosts": excludedHosts,
		"results":       results,
	})
}

// handleMe: GET /api/me
func (s *AgentServer) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	uf, err := s.readUsers()
	myIP := s.detectedIP

	name := ""
	registered := false
	if err == nil {
		for _, u := range uf.Users {
			if ipMatches(u.IP, myIP) {
				name = u.Name
				registered = true
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ip":         myIP,
		"name":       name,
		"registered": registered,
	})
}

// handleUsers: GET /api/users
func (s *AgentServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	uf, err := s.readUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, uf)
}

// handleRegister: POST /api/users/register
func (s *AgentServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req User
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.Name == "" || req.IP == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and ip are required"})
		return
	}
	if req.Role == "" {
		req.Role = "작업자"
	}

	uf, err := s.readUsers()
	if err != nil {
		uf = UsersFile{Users: []User{}}
	}

	updated := false
	for i, u := range uf.Users {
		if ipMatches(u.IP, req.IP) {
			uf.Users[i].Name = req.Name
			if req.Role != "" {
				uf.Users[i].Role = req.Role
			}
			updated = true
			break
		}
	}
	if !updated {
		uf.Users = append(uf.Users, req)
	}

	if err := s.writeUsers(uf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist users.json"})
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// handleSessionCreate: POST /api/session/create
func (s *AgentServer) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var sess Session
	if err := json.NewDecoder(r.Body).Decode(&sess); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	sess.SessionId = generateSessionID()
	sess.Status = "PENDING"
	sess.CreatedAt = time.Now().Format(time.RFC3339)
	if sess.CheckerHistory == nil {
		sess.CheckerHistory = []CheckerHistoryItem{}
	}

	if err := s.saveSession(&sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save session: " + err.Error()})
		return
	}

	// Async direct HTTP notification to checker (자기 자신 호출 방지)
	if sess.Checker.IP != "" && sess.Checker.IP != s.detectedIP && sess.Checker.IP != "127.0.0.1" && sess.Checker.IP != sess.Worker.IP {
		go func(checkerIP string, sessCopy Session) {
			url := fmt.Sprintf("http://%s:%d/api/peer/notify", checkerIP, s.config.ListenPort)
			s.postPeerJSON(url, map[string]interface{}{
				"type":      "NEW_SESSION",
				"sessionId": sessCopy.SessionId,
				"session":   sessCopy,
			})
		}(sess.Checker.IP, sess)
	}

	// Broadcast event
	s.broadcastEvent("NEW_SESSION", sess.SessionId, sess)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessionId": sess.SessionId,
		"status":    sess.Status,
	})
}

// handleSessionStatus: GET /api/session/status?sessionId=...
func (s *AgentServer) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionId := r.URL.Query().Get("sessionId")
	if sessionId == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId query parameter required"})
		return
	}

	sess, err := s.loadSession(sessionId)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	clientIP := getClientIP(r)

	// Authorization Check: Must be worker or checker
	isWorker := ipMatches(clientIP, sess.Worker.IP)
	isChecker := ipMatches(clientIP, sess.Checker.IP)

	if !isWorker && !isChecker {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: unauthorized IP address"})
		return
	}

	// Blind preview check
	// If client is checker (or requesting as checker) AND status is PENDING or CHECKING AND NOT comparing
	isComparing := r.URL.Query().Get("compare") == "true"
	asChecker := r.URL.Query().Get("asChecker") == "true"
	needsBlinding := (isChecker || asChecker) && !isComparing && (sess.Status == "PENDING" || sess.Status == "CHECKING")

	if needsBlinding {
		detailPrev := makeHostPreview(sess.WorkerData.DetailHosts)
		incidentPrev := makeHostPreview(sess.WorkerData.IncidentHosts)
		targetPrev := makeHostPreview(sess.WorkerData.TargetHosts)
		awxCount := len(sess.WorkerData.AwxHosts)

		blindedWorkerData := map[string]interface{}{
			"awxCount":             awxCount,
			"rawAwxCount":          sess.WorkerData.RawAwxCount,
			"awxHosts":             []string{},
			"excludedHosts":        []string{},
			"detailHosts":          []string{},
			"incidentHosts":        []string{},
			"targetHosts":          []string{},
			"detailHostsPreview":   detailPrev,
			"incidentHostsPreview": incidentPrev,
			"targetHostsPreview":   targetPrev,
			"preview": map[string]interface{}{
				"detail":   detailPrev,
				"incident": incidentPrev,
				"target":   targetPrev,
			},
		}

		response := map[string]interface{}{
			"sessionId":      sess.SessionId,
			"status":         sess.Status,
			"createdAt":      sess.CreatedAt,
			"decidedAt":      sess.DecidedAt,
			"worker":         sess.Worker,
			"checker":        sess.Checker,
			"metadata":       sess.Metadata,
			"activeFields":   sess.ActiveFields,
			"workerData":     blindedWorkerData,
			"blinded":        true,
			"checkerData":    sess.CheckerData,
			"checkerHistory": sess.CheckerHistory,
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	// Unblinded status response
	writeJSON(w, http.StatusOK, sess)
}

// handleSessionList: GET /api/session/list
func (s *AgentServer) handleSessionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := getClientIP(r)
	pattern := filepath.Join(s.config.SharedDir, "sessions", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": []interface{}{}})
		return
	}

	var results []interface{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		isWorker := ipMatches(clientIP, sess.Worker.IP)
		isChecker := ipMatches(clientIP, sess.Checker.IP)

		if clientIP != "" && !isWorker && !isChecker {
			continue
		}

		if isChecker && !isWorker && (sess.Status == "PENDING" || sess.Status == "CHECKING") {
			detailPrev := makeHostPreview(sess.WorkerData.DetailHosts)
			incidentPrev := makeHostPreview(sess.WorkerData.IncidentHosts)
			targetPrev := makeHostPreview(sess.WorkerData.TargetHosts)
			blinded := map[string]interface{}{
				"sessionId":      sess.SessionId,
				"status":         sess.Status,
				"createdAt":      sess.CreatedAt,
				"decidedAt":      sess.DecidedAt,
				"worker":         sess.Worker,
				"checker":        sess.Checker,
				"metadata":       sess.Metadata,
				"activeFields":   sess.ActiveFields,
				"blinded":        true,
				"workerData": map[string]interface{}{
					"awxCount":             len(sess.WorkerData.AwxHosts),
					"rawAwxCount":          sess.WorkerData.RawAwxCount,
					"detailHostsPreview":   detailPrev,
					"incidentHostsPreview": incidentPrev,
					"targetHostsPreview":   targetPrev,
				},
				"checkerData":    sess.CheckerData,
				"checkerHistory": sess.CheckerHistory,
			}
			results = append(results, blinded)
		} else {
			results = append(results, sess)
		}
	}

	if results == nil {
		results = []interface{}{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": results})
}

// handleSessionReassign: POST /api/session/reassign
func (s *AgentServer) handleSessionReassign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionId  string `json:"sessionId"`
		NewChecker User   `json:"newChecker"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.SessionId == "" || req.NewChecker.IP == "" || req.NewChecker.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId, newChecker name and ip required"})
		return
	}

	sess, err := s.loadSession(req.SessionId)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if sess.Status != "PENDING" && sess.Status != "CHECKING" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("cannot reassign session in status %s", sess.Status)})
		return
	}

	oldChecker := sess.Checker
	sess.CheckerHistory = append(sess.CheckerHistory, CheckerHistoryItem{
		Checker:      oldChecker,
		ReassignedAt: time.Now().Format(time.RFC3339),
	})
	sess.Checker = req.NewChecker

	if err := s.saveSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save updated session"})
		return
	}

	// Notify old checker async (자기 자신 호출 방지)
	if oldChecker.IP != "" && oldChecker.IP != s.detectedIP && oldChecker.IP != "127.0.0.1" && oldChecker.IP != sess.Worker.IP {
		go func(ip string, sid string) {
			url := fmt.Sprintf("http://%s:%d/api/peer/notify", ip, s.config.ListenPort)
			s.postPeerJSON(url, map[string]interface{}{
				"type":      "REASSIGNED_AWAY",
				"sessionId": sid,
				"data": map[string]interface{}{
					"newChecker": req.NewChecker,
				},
			})
		}(oldChecker.IP, sess.SessionId)
	}

	// Notify new checker async (자기 자신 호출 방지)
	if req.NewChecker.IP != "" && req.NewChecker.IP != s.detectedIP && req.NewChecker.IP != "127.0.0.1" && req.NewChecker.IP != sess.Worker.IP {
		go func(ip string, sessCopy Session) {
			url := fmt.Sprintf("http://%s:%d/api/peer/notify", ip, s.config.ListenPort)
			s.postPeerJSON(url, map[string]interface{}{
				"type":      "NEW_SESSION",
				"sessionId": sessCopy.SessionId,
				"session":   sessCopy,
			})
		}(req.NewChecker.IP, *sess)
	}

	// Broadcast event
	s.broadcastEvent("SESSION_REASSIGNED", sess.SessionId, sess)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"checker": sess.Checker,
	})
}

// handleSessionDecision: POST /api/session/decision
func (s *AgentServer) handleSessionDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionId   string      `json:"sessionId"`
		CheckerIp   string      `json:"checkerIp"`
		CheckerData CheckerData `json:"checkerData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	sess, err := s.loadSession(req.SessionId)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	// Identify client IP
	clientIP := getClientIP(r)
	effectiveCheckerIP := req.CheckerIp
	if effectiveCheckerIP == "" {
		effectiveCheckerIP = clientIP
	}

	// Validate client matches session checker IP
	if !ipMatches(effectiveCheckerIP, sess.Checker.IP) && !ipMatches(clientIP, sess.Checker.IP) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: checker IP mismatch"})
		return
	}

	// Validation rule: APPROVED requires isAllMatch == true
	if req.CheckerData.Decision == "APPROVED" {
		if !req.CheckerData.IsAllMatch {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "cannot approve session when isAllMatch is false",
			})
			return
		}
	} else if req.CheckerData.Decision != "REJECTED" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "decision must be APPROVED or REJECTED",
		})
		return
	}

	sess.Status = req.CheckerData.Decision
	sess.DecidedAt = time.Now().Format(time.RFC3339)
	sess.CheckerData = &req.CheckerData

	if err := s.saveSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save decision: " + err.Error()})
		return
	}

	// Direct HTTP POST update to worker
	if sess.Worker.IP != "" {
		go func(workerIP string, sessCopy Session) {
			url := fmt.Sprintf("http://%s:%d/api/peer/update", workerIP, s.config.ListenPort)
			s.postPeerJSON(url, map[string]interface{}{
				"type":        "SESSION_DECIDED",
				"sessionId":   sessCopy.SessionId,
				"status":      sessCopy.Status,
				"checkerData": sessCopy.CheckerData,
			})
		}(sess.Worker.IP, *sess)
	}

	// Broadcast event
	s.broadcastEvent("SESSION_DECIDED", sess.SessionId, sess)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  sess.Status,
	})
}

// handlePeerNotify: POST /api/peer/notify
func (s *AgentServer) handlePeerNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	var payload struct {
		Type      string      `json:"type"`
		SessionId string      `json:"sessionId"`
		Session   interface{} `json:"session"`
		Data      interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		s.broadcastEvent(payload.Type, payload.SessionId, payload)

		// Auto browser launch if requested and configured
		if s.config.AutoOpenBrowserOnRequest && payload.Type == "NEW_SESSION" && payload.SessionId != "" {
			url := fmt.Sprintf("http://localhost:%d/checker?sessionId=%s", s.config.ListenPort, payload.SessionId)
			go openBrowser(url)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"received": true})
}

// handlePeerUpdate: POST /api/peer/update
func (s *AgentServer) handlePeerUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	var payload struct {
		Type      string      `json:"type"`
		SessionId string      `json:"sessionId"`
		Status    string      `json:"status"`
		Data      interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		s.broadcastEvent("PEER_UPDATE", payload.SessionId, payload)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"received": true})
}

// handleEvents: GET /api/events (Server-Sent Events)
func (s *AgentServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")

	eventChan := make(chan SSEEvent, 32)
	s.sseBroker.AddClient(eventChan)
	defer s.sseBroker.RemoveClient(eventChan)

	// Send initial ping
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	keepAliveTicker := time.NewTicker(15 * time.Second)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.stopChan:
			return
		case <-keepAliveTicker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case evt := <-eventChan:
			dataBytes, err := json.Marshal(evt)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", dataBytes)
				flusher.Flush()
			}
		}
	}
}

// handleCheckerUI: GET /checker
func (s *AgentServer) handleCheckerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(checkerHTMLTemplate))
}

// handleWebSocket handles WebSocket connections for real-time bidirectional communication.
func (s *AgentServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrade(w, r)
	if err != nil {
		return
	}
	client := &WSClient{conn: conn}
	s.wsHub.Add(client)
	defer func() {
		s.wsHub.Remove(client)
		conn.Close()
	}()

	for {
		opcode, payload, err := wsReadMessage(conn)
		if err != nil {
			break
		}
		if opcode == 0x08 { // Close frame
			break
		}
		if opcode == 0x09 { // Ping frame
			_ = client.WritePong(payload)
			continue
		}
		if opcode == 0x01 { // Text frame
			go s.processWSRequest(client, payload)
		}
	}
}

// processWSRequest routes WebSocket requests to existing HTTP handlers.
func (s *AgentServer) processWSRequest(client *WSClient, msg []byte) {
	var req WSRequestMsg
	if err := json.Unmarshal(msg, &req); err != nil {
		resp, _ := json.Marshal(WSResponseMsg{Status: http.StatusBadRequest, Error: "invalid JSON format"})
		_ = client.WriteText(resp)
		return
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	httpReq, err := http.NewRequest(strings.ToUpper(req.Method), req.Path, bodyReader)
	if err != nil {
		resp, _ := json.Marshal(WSResponseMsg{ID: req.ID, Status: http.StatusBadRequest, Error: err.Error()})
		_ = client.WriteText(resp)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	rec := newWSResponseRecorder()
	s.Routes().ServeHTTP(rec, httpReq)

	var data interface{}
	if rec.body.Len() > 0 {
		if err := json.Unmarshal(rec.body.Bytes(), &data); err != nil {
			data = rec.body.String()
		}
	}

	resp := WSResponseMsg{
		ID:     req.ID,
		Status: rec.code,
		Data:   data,
	}
	if rec.code >= 400 && data != nil {
		if m, ok := data.(map[string]interface{}); ok {
			if errStr, hasErr := m["error"].(string); hasErr {
				resp.Error = errStr
			}
		}
	}
	respBytes, _ := json.Marshal(resp)
	_ = client.WriteText(respBytes)
}

// openBrowser launches the system default browser.
func openBrowser(targetUrl string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", targetUrl)
	case "darwin":
		cmd = exec.Command("open", targetUrl)
	default:
		cmd = exec.Command("xdg-open", targetUrl)
	}
	_ = cmd.Start()
}

// checkerHTMLTemplate contains the high-contrast Argon-style checker UI.
const checkerHTMLTemplate = `<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AWX 작업대상 호스트 교차 검증기 - 확인자 콘솔</title>
<style>
:root {
  --bg-main: #0f172a;
  --bg-card: #1e293b;
  --bg-card-alt: #334155;
  --text-main: #f8fafc;
  --text-muted: #94a3b8;
  --border-color: #475569;
  --accent-blue: #38bdf8;
  --accent-green: #22c55e;
  --accent-red: #ef4444;
  --accent-amber: #f59e0b;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: Pretendard, 'Malgun Gothic', Consolas, sans-serif;
  background-color: var(--bg-main);
  color: var(--text-main);
  padding: 24px;
  line-height: 1.5;
}
.container { max-width: 960px; margin: 0 auto; }
.header {
  display: flex; justify-content: space-between; align-items: center;
  padding-bottom: 16px; border-bottom: 1px solid var(--border-color);
  margin-bottom: 24px;
}
.header h1 { font-size: 1.25rem; font-weight: 700; color: var(--accent-blue); }
.status-badge {
  padding: 4px 12px; border-radius: 4px; font-weight: bold; font-size: 0.85rem;
  text-transform: uppercase;
}
.status-PENDING { background-color: var(--accent-amber); color: #000; }
.status-CHECKING { background-color: var(--accent-blue); color: #000; }
.status-APPROVED { background-color: var(--accent-green); color: #fff; }
.status-REJECTED { background-color: var(--accent-red); color: #fff; }

.card {
  background: var(--bg-card); border: 1px solid var(--border-color);
  border-radius: 6px; padding: 18px; margin-bottom: 20px;
}
.card-title {
  font-size: 0.95rem; font-weight: 600; margin-bottom: 12px;
  color: var(--text-muted); border-bottom: 1px solid var(--border-color);
  padding-bottom: 6px;
}
.meta-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; font-size: 0.9rem; }
.meta-item { display: flex; flex-direction: column; }
.meta-label { color: var(--text-muted); font-size: 0.8rem; margin-bottom: 2px; }
.meta-val { font-weight: 500; word-break: break-all; }

.section-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 12px; }
.field-box { background: var(--bg-card-alt); border-radius: 4px; padding: 12px; }
.field-header { display: flex; justify-content: space-between; font-size: 0.85rem; margin-bottom: 6px; font-weight: 600; }
textarea, input[type="text"] {
  width: 100%; background: #0f172a; border: 1px solid var(--border-color);
  color: #f8fafc; padding: 8px; border-radius: 4px; font-family: Consolas, monospace;
  font-size: 0.85rem; resize: vertical;
}
textarea:focus, input:focus { outline: none; border-color: var(--accent-blue); }

.preview-list {
  background: #0f172a; border: 1px dashed var(--border-color);
  border-radius: 4px; padding: 8px; font-family: Consolas, monospace; font-size: 0.82rem;
  min-height: 80px;
}
.preview-item { padding: 2px 4px; border-bottom: 1px solid #1e293b; }
.preview-item:last-child { border-bottom: none; }
.blind-note { color: var(--accent-amber); font-size: 0.78rem; margin-top: 6px; }

.match-result-card {
  padding: 14px; border-radius: 6px; text-align: center; font-weight: bold;
  font-size: 1.05rem; margin: 16px 0;
}
.match-ok { background: #064e3b; color: #6ee7b7; border: 1px solid #059669; }
.match-fail { background: #7f1d1d; color: #fca5a5; border: 1px solid #dc2626; }

.btn-group { display: flex; gap: 12px; margin-top: 20px; }
.btn {
  flex: 1; padding: 12px; border: none; border-radius: 4px; font-weight: bold;
  font-size: 0.95rem; cursor: pointer; transition: opacity 0.2s;
}
.btn:hover { opacity: 0.9; }
.btn-approve { background: var(--accent-green); color: #fff; }
.btn-approve:disabled { background: #4b5563; color: #9ca3af; cursor: not-allowed; }
.btn-reject { background: var(--accent-red); color: #fff; }
.btn-compare { background: var(--accent-blue); color: #000; }
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <div>
      <h1>AWX 작업대상 호스트 교차 검증기</h1>
      <p style="font-size: 0.85rem; color: var(--text-muted);">확인자 독립 검증 및 상호 대조 콘솔</p>
    </div>
    <div id="sessionStatus" class="status-badge status-PENDING">대기중 (PENDING)</div>
  </div>

  <div class="card">
    <div class="card-title">세션 메타데이터 & 작업자 정보</div>
    <div class="meta-grid">
      <div class="meta-item"><span class="meta-label">세션 ID</span><span id="metaSessionId" class="meta-val">-</span></div>
      <div class="meta-item"><span class="meta-label">작업자</span><span id="metaWorker" class="meta-val">-</span></div>
      <div class="meta-item"><span class="meta-label">인시던트 ID</span><span id="metaIncident" class="meta-val">-</span></div>
      <div class="meta-item"><span class="meta-label">특이사항</span><span id="metaNotes" class="meta-val">-</span></div>
    </div>
  </div>

  <div class="card">
    <div class="card-title">작업자 제출 호스트 (블라인드 사전 검토)</div>
    <div class="section-grid">
      <div class="field-box">
        <div class="field-header"><span>상세목록현황 호스트</span><span id="workerDetailCount">0대</span></div>
        <div id="workerDetailPreview" class="preview-list">데이터 로딩중...</div>
        <div id="detailBlindNote" class="blind-note"></div>
      </div>
      <div class="field-box">
        <div class="field-header"><span>인시던트 호스트</span><span id="workerIncidentCount">0대</span></div>
        <div id="workerIncidentPreview" class="preview-list">데이터 로딩중...</div>
        <div id="incidentBlindNote" class="blind-note"></div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-title">확인자 직접 대조 입력 (교차 확인)</div>
    <p style="font-size: 0.82rem; color: var(--text-muted); margin-bottom: 12px;">
      확인자는 독립적으로 수신한 호스트 목록을 아래에 입력하여 교차 검증을 수행합니다.
    </p>
    <div class="section-grid">
      <div class="field-box">
        <div class="field-header"><span>확인자 상세목록 호스트 입력</span></div>
        <textarea id="checkerDetailInput" rows="5" placeholder="확인자 상세목록 호스트 붙여넣기..."></textarea>
      </div>
      <div class="field-box">
        <div class="field-header"><span>확인자 인시던트 호스트 입력</span></div>
        <textarea id="checkerIncidentInput" rows="5" placeholder="확인자 인시던트 호스트 붙여넣기..."></textarea>
      </div>
    </div>
    <div style="margin-top: 14px;">
      <button class="btn btn-compare" id="btnRunCompare">교차 비교 대조 실행</button>
    </div>
    <div id="matchResult" class="match-result-card" style="display: none;"></div>
  </div>

  <div class="card">
    <div class="card-title">검증 결정 및 의견</div>
    <input type="text" id="decisionComment" placeholder="결정 사유 또는 작업 특이사항을 입력하세요 (반려 시 필수)">
    <div class="btn-group">
      <button id="btnApprove" class="btn btn-approve" disabled>작업 승인 (APPROVE)</button>
      <button id="btnReject" class="btn btn-reject">작업 반려 (REJECT)</button>
    </div>
  </div>
</div>

<script>
let currentSession = null;
let isAllMatch = false;
const urlParams = new URLSearchParams(window.location.search);
const sessionId = urlParams.get('sessionId');

function parseHosts(text) {
  if (!text) return [];
  return text.split(/[\r\n,;\t ]+/)
    .map(s => s.trim())
    .filter(s => s.length > 0 && !s.includes('/'));
}

async function loadSession() {
  if (!sessionId) return;
  try {
    const res = await fetch('/api/session/status?sessionId=' + sessionId);
    if (!res.ok) {
      alert('세션을 불러올 수 없습니다: HTTP ' + res.status);
      return;
    }
    currentSession = await res.json();
    renderSession(currentSession);
  } catch (e) {
    console.error('세션 로딩 실패', e);
  }
}

function renderSession(s) {
  document.getElementById('metaSessionId').textContent = s.sessionId;
  document.getElementById('metaWorker').textContent = (s.worker?.name || '') + ' (' + (s.worker?.ip || '') + ')';
  document.getElementById('metaIncident').textContent = s.metadata?.incidentId || '-';
  document.getElementById('metaNotes').textContent = s.metadata?.specialNotes || '-';

  const statusEl = document.getElementById('sessionStatus');
  statusEl.className = 'status-badge status-' + s.status;
  statusEl.textContent = s.status;

  // Blind preview rendering
  const wd = s.workerData || {};
  const dp = wd.detailHostsPreview || wd.detailPreview || (wd.preview && wd.preview.detail);
  const ip = wd.incidentHostsPreview || wd.incidentPreview || (wd.preview && wd.preview.incident);

  if (dp) {
    document.getElementById('workerDetailCount').textContent = dp.totalCount + '대';
    document.getElementById('workerDetailPreview').innerHTML = dp.previewHosts.map(h => '<div class="preview-item">' + h + '</div>').join('');
    if (dp.blindedCount > 0) {
      document.getElementById('detailBlindNote').textContent = '⚠️ 보안 블라인드 적용: 상위 3대만 표시 (나머지 ' + dp.blindedCount + '대 숨김)';
    }
  }
  if (ip) {
    document.getElementById('workerIncidentCount').textContent = ip.totalCount + '대';
    document.getElementById('workerIncidentPreview').innerHTML = ip.previewHosts.map(h => '<div class="preview-item">' + h + '</div>').join('');
    if (ip.blindedCount > 0) {
      document.getElementById('incidentBlindNote').textContent = '⚠️ 보안 블라인드 적용: 상위 3대만 표시 (나머지 ' + ip.blindedCount + '대 숨김)';
    }
  }

  if (s.status === 'APPROVED' || s.status === 'REJECTED') {
    document.getElementById('btnApprove').disabled = true;
    document.getElementById('btnReject').disabled = true;
    if (s.checkerData) {
      document.getElementById('decisionComment').value = s.checkerData.decisionComment || '';
    }
  }
}

document.getElementById('btnRunCompare').addEventListener('click', async () => {
  if (!sessionId) return;
  // Request full data for compare
  const res = await fetch('/api/session/status?sessionId=' + sessionId + '&compare=true');
  if (!res.ok) {
    alert('비교 데이터를 불러올 수 없습니다.');
    return;
  }
  const full = await res.json();
  const cDetail = parseHosts(document.getElementById('checkerDetailInput').value);
  const cIncident = parseHosts(document.getElementById('checkerIncidentInput').value);
  const wDetail = full.workerData?.detailHosts || [];
  const wIncident = full.workerData?.incidentHosts || [];

  const detailMatch = JSON.stringify([...cDetail].sort()) === JSON.stringify([...wDetail].sort());
  const incidentMatch = JSON.stringify([...cIncident].sort()) === JSON.stringify([...wIncident].sort());
  isAllMatch = detailMatch && incidentMatch && cDetail.length > 0;

  const resultEl = document.getElementById('matchResult');
  resultEl.style.display = 'block';
  if (isAllMatch) {
    resultEl.className = 'match-result-card match-ok';
    resultEl.textContent = '⭕ 교차 대조 결과: 전수 일치 (MATCH) - 승인 가능';
    document.getElementById('btnApprove').disabled = false;
  } else {
    resultEl.className = 'match-result-card match-fail';
    resultEl.textContent = '❌ 교차 대조 결과: 호스트 불일치 (MISMATCH) - 상세/인시던트 목록을 확인하세요';
    document.getElementById('btnApprove').disabled = true;
  }
});

document.getElementById('btnApprove').addEventListener('click', () => sendDecision('APPROVED'));
document.getElementById('btnReject').addEventListener('click', () => sendDecision('REJECTED'));

async function sendDecision(decision) {
  const comment = document.getElementById('decisionComment').value.trim();
  if (decision === 'REJECTED' && !comment) {
    alert('반려 시에는 반드시 사유를 입력해야 합니다.');
    return;
  }
  const cDetail = parseHosts(document.getElementById('checkerDetailInput').value);
  const cIncident = parseHosts(document.getElementById('checkerIncidentInput').value);

  const payload = {
    sessionId: sessionId,
    checkerData: {
      detailHosts: cDetail,
      incidentHosts: cIncident,
      targetHosts: [],
      isAllMatch: isAllMatch,
      decision: decision,
      decisionComment: comment
    }
  };

  const res = await fetch('/api/session/decision', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    alert('결정 처리 실패: ' + (err.error || res.statusText));
    return;
  }

  alert('결정이 저장되었습니다: ' + decision);
  loadSession();
}

// SSE Connection
const evtSource = new EventSource('/api/events');
evtSource.onmessage = function(e) {
  try {
    const data = JSON.parse(e.data);
    if (data.sessionId === sessionId || (data.data && data.data.sessionId === sessionId)) {
      loadSession();
    }
  } catch (_) {}
};

loadSession();
</script>
</body>
</html>
`

func waitForServerReady(healthURL string, timeout time.Duration) {
	client := &http.Client{
		Timeout: 300 * time.Millisecond,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func main() {
	portFlag := flag.Int("port", 18080, "Listen port for HTTP server")
	shareFlag := flag.String("share", ".", "Shared directory path")
	autoBrowserFlag := flag.Bool("auto-browser", false, "Auto open browser on peer notification")
	bindIPFlag := flag.String("bind-ip", "", "Override detected IP address")
	headlessFlag := flag.Bool("headless", false, "Run server only without launching desktop app window")
	openAppFlag := flag.Bool("open-app", false, "Force open desktop app window even in test mode")
	flag.Parse()

	cfg := Config{
		SharedDir:                *shareFlag,
		ListenPort:               *portFlag,
		PollIntervalMs:           500,
		AutoOpenBrowserOnRequest: *autoBrowserFlag,
		BindIP:                   *bindIPFlag,
		Headless:                 *headlessFlag,
	}

	// Determine exe directory for robust relative path resolution (e.g. UNC shares, desktop shortcuts)
	exeDir := "."
	if exePath, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exePath)
	}

	// If -share was not explicitly passed, check if config.json or users.json exists in the exe's directory
	if !isFlagPassed("share") && (cfg.SharedDir == "." || cfg.SharedDir == "") {
		if _, err := os.Stat(filepath.Join(exeDir, "config.json")); err == nil {
			cfg.SharedDir = exeDir
		} else if _, err := os.Stat(filepath.Join(exeDir, "users.json")); err == nil {
			cfg.SharedDir = exeDir
		}
	}

	// Try reading config.json from sharedDir or exeDir
	configPath := filepath.Join(cfg.SharedDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(exeDir, "config.json")
	}

	if fileBytes, err := os.ReadFile(configPath); err == nil {
		var fileCfg Config
		if err := json.Unmarshal(fileBytes, &fileCfg); err == nil {
			if !isFlagPassed("port") && fileCfg.ListenPort != 0 {
				cfg.ListenPort = fileCfg.ListenPort
			}
			if !isFlagPassed("share") && fileCfg.SharedDir != "" {
				cfg.SharedDir = fileCfg.SharedDir
			}
			if !isFlagPassed("auto-browser") {
				cfg.AutoOpenBrowserOnRequest = fileCfg.AutoOpenBrowserOnRequest
			}
			if fileCfg.PollIntervalMs > 0 {
				cfg.PollIntervalMs = fileCfg.PollIntervalMs
			}
			if !isFlagPassed("headless") && fileCfg.Headless {
				cfg.Headless = fileCfg.Headless
			}
		}
	}

	// If cfg.SharedDir is still relative (e.g. "." or empty), resolve it relative to exeDir
	if !filepath.IsAbs(cfg.SharedDir) && !strings.HasPrefix(cfg.SharedDir, `\\`) {
		cfg.SharedDir = filepath.Clean(filepath.Join(exeDir, cfg.SharedDir))
	}

	// Determine if app window should be launched:
	// 1) Explicit --headless flag suppresses window.
	// 2) If launched as cross-checker-agent.exe with -bind-ip and without -open-app (automated test scripts), run headless.
	shouldOpenApp := !cfg.Headless
	exeBase := strings.ToLower(filepath.Base(os.Args[0]))
	if strings.Contains(exeBase, "agent") && isFlagPassed("bind-ip") && !*openAppFlag {
		shouldOpenApp = false
	}

	server, err := NewAgentServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize agent server: %v", err)
	}

	server.StartBackgroundPolling()
	defer server.Stop()

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.ListenPort)
	log.Printf("========================================================\n")
	log.Printf("  AWX Host Cross-Checker 서버 기동 완료\n")
	log.Printf("  >> 브라우저 접속 주소: http://127.0.0.1:%d/\n", cfg.ListenPort)
	log.Printf("  >> 감지된 내 PC IP:    %s\n", server.detectedIP)
	log.Printf("  >> 공유폴더 경로:      %s\n", cfg.SharedDir)
	log.Printf("  (주의: 0.0.0.0 이 아니라 반드시 127.0.0.1 로 접속해야 합니다)\n")
	log.Printf("========================================================\n")

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// If another instance is already running on this port, bring up the window and exit cleanly
		client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
		if resp, hErr := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", cfg.ListenPort)); hErr == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			log.Printf("이미 포트 %d에서 실행 중입니다. 기존 실행 중인 앱 창을 엽니다.\n", cfg.ListenPort)
			if shouldOpenApp {
				_, _ = launcher.Open(fmt.Sprintf("http://127.0.0.1:%d/", cfg.ListenPort))
			}
			return
		}
		log.Fatalf("포트 바인딩 실패 (포트 %d 이미 사용 중): %v", cfg.ListenPort, err)
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: server.Routes(),
	}

	// Warm up AWX inventory 3 cache in background
	go server.WarmupAwxCache("3")

	shutdownChan := make(chan struct{})
	var shutdownOnce sync.Once
	shutdownServer := func() {
		shutdownOnce.Do(func() {
			log.Println("Shutting down AWX Host Cross-Checker...")
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
			server.Stop()
			close(shutdownChan)
			os.Exit(0)
		})
	}

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server stopped with error: %v", err)
		}
	}()

	// 1) 로컬 HTTP 서버가 100% 응답 가능한 상태가 될 때까지 동기적으로 먼저 확인!
	waitForServerReady(fmt.Sprintf("http://127.0.0.1:%d/api/health", cfg.ListenPort), 5*time.Second)

	// 2) 앱 창 실행 및 라이프사이클 감시
	if shouldOpenApp {
		server.StartLifecycleWatcher(func() {
			log.Println("Client window closed. Terminating process.")
			shutdownServer()
		})

		appURL := fmt.Sprintf("http://127.0.0.1:%d/", cfg.ListenPort)
		browserUsed, err := launcher.Open(appURL)
		if err != nil {
			log.Printf("Failed to open app window: %v\n", err)
		} else {
			log.Printf("Launched desktop app window via %s (%s)\n", browserUsed, appURL)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		shutdownServer()
	case <-shutdownChan:
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
