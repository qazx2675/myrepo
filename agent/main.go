package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
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
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

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
	AwxToken                 string `json:"awxToken,omitempty"`
	AwxDefaultScope          string `json:"awxDefaultScope,omitempty"`
	AwxInsecureTLS           *bool  `json:"awxInsecureTls,omitempty"`
	Headless                 bool   `json:"headless,omitempty"`
}

// AwxBridgeState tracks the active open Edge/Chrome AWX tab connected via WebSocket bridge.
type AwxBridgeState struct {
	Connected bool      `json:"connected"`
	Source    string    `json:"source,omitempty"`
	URL       string    `json:"url,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Hash      string    `json:"hash,omitempty"`
	Label     string    `json:"label,omitempty"`
	ScopeType string    `json:"scopeType,omitempty"`
	ScopeID   string    `json:"scopeId,omitempty"`
	Page      int       `json:"page,omitempty"`
	PageHosts []string  `json:"pageHosts,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// AwxBridgeResult holds async collection results returned from a bridged AWX browser tab.
type AwxBridgeResult struct {
	RequestID string   `json:"requestId"`
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	Hosts     []string `json:"hosts,omitempty"`
	RawCount  int      `json:"rawCount,omitempty"`
	Label     string   `json:"label,omitempty"`
	ScopeType string   `json:"scopeType,omitempty"`
	ScopeID   string   `json:"scopeId,omitempty"`
	Page      int      `json:"page,omitempty"`
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
	conn     net.Conn
	mu       sync.Mutex
	IsBridge bool
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

func (h *WSHub) BridgeCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	cnt := 0
	for c := range h.clients {
		if c.IsBridge {
			cnt++
		}
	}
	return cnt
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
	config           Config
	awxCfgMu         sync.RWMutex
	awxSessionMu     sync.Mutex
	awxSessionKey    string
	awxSessionClient *http.Client
	usersMutex       sync.RWMutex
	sessMutex        sync.RWMutex
	sseBroker        *SSEBroker
	wsHub            *WSHub
	httpClient       *http.Client
	awxHttpClient    *http.Client
	awxCacheMu       sync.RWMutex
	awxCache         map[string]awxCacheEntry
	bridgeMu         sync.RWMutex
	bridgeState      AwxBridgeState
	bridgePending    map[string]chan AwxBridgeResult
	stopChan         chan struct{}
	fileModTime      map[string]time.Time
	modMu            sync.Mutex
	detectedIP       string
	lifecycleMu      sync.Mutex
	hasSeenClient    bool
	lastPingTime     time.Time
	byeReceived      bool
	byeTime          time.Time
	onShutdown       func()
	lastUsersSync    time.Time
	usersSource      string
}

const awxRequestUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

func newAwxHTTPClient(insecureTLS bool) *http.Client {
	jar, _ := cookiejar.New(nil)
	minTLS := uint16(tls.VersionTLS12)
	if insecureTLS {
		minTLS = tls.VersionTLS10
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			origin := via[0]
			if origin.Header.Get("User-Agent") != "" && req.Header.Get("User-Agent") == "" {
				req.Header.Set("User-Agent", origin.Header.Get("User-Agent"))
			}
			if origin.Header.Get("Accept") != "" && req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", origin.Header.Get("Accept"))
			}
			// Preserve Authorization across scheme/IP redirects within AWX API paths.
			if auth := origin.Header.Get("Authorization"); auth != "" {
				sameHost := strings.EqualFold(req.URL.Hostname(), origin.URL.Hostname())
				origWasIP := net.ParseIP(origin.URL.Hostname()) != nil
				isApiPath := strings.Contains(req.URL.Path, "/api/")
				if isApiPath && (sameHost || origWasIP) {
					req.Header.Set("Authorization", auth)
				}
			}
			return nil
		},
		Transport: &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureTLS,
				MinVersion:         minTLS,
			},
		},
	}
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
	if cfg.AwxToken == "" {
		if envTok := os.Getenv("AWX_TOKEN"); envTok != "" {
			cfg.AwxToken = strings.TrimSpace(envTok)
		}
	}
	if cfg.AwxDefaultScope == "" {
		if envScope := os.Getenv("AWX_DEFAULT_INVENTORY"); envScope != "" {
			cfg.AwxDefaultScope = strings.TrimSpace(envScope)
		} else {
			cfg.AwxDefaultScope = "3"
		}
	}
	insecureTLS := true
	if cfg.AwxInsecureTLS != nil {
		insecureTLS = *cfg.AwxInsecureTLS
	} else {
		t := true
		cfg.AwxInsecureTLS = &t
	}
	if parsedBase, parsedScope, _ := parseAwxTargetInput(cfg.AwxBaseURL); parsedBase != "" {
		cfg.AwxBaseURL = parsedBase
		if parsedScope != "" && !isExplicitScope(cfg.AwxDefaultScope) {
			cfg.AwxDefaultScope = parsedScope
		}
	}

	sessionsDir := filepath.Join(cfg.SharedDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		log.Printf("[경고] 공유폴더(%s) sessions 접근 실패: %v, 로컬 폴더로 폴백 시도합니다.\n", cfg.SharedDir, err)
		if exePath, eErr := os.Executable(); eErr == nil {
			fallbackDir := filepath.Dir(exePath)
			if fallbackDir != cfg.SharedDir {
				cfg.SharedDir = fallbackDir
				sessionsDir = filepath.Join(cfg.SharedDir, "sessions")
				if fErr := os.MkdirAll(sessionsDir, 0755); fErr != nil {
					return nil, fmt.Errorf("공유폴더 및 로컬 세션 폴더 생성 실패: %w (원래 오류: %v)", fErr, err)
				}
				log.Printf("[안내] 로컬 세션 폴더 사용: %s\n", sessionsDir)
			} else {
				return nil, fmt.Errorf("세션 폴더 생성 실패: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to create sessions directory: %w", err)
		}
	}

	detectedIP := detectLocalIP(cfg.BindIP)

	s := &AgentServer{
		config:        cfg,
		sseBroker:     newSSEBroker(),
		wsHub:         newWSHub(),
		httpClient:    &http.Client{Timeout: 1 * time.Second},
		awxHttpClient: newAwxHTTPClient(insecureTLS),
		awxCache:      make(map[string]awxCacheEntry),
		bridgePending: make(map[string]chan AwxBridgeResult),
		stopChan:      make(chan struct{}),
		fileModTime:   make(map[string]time.Time),
		detectedIP:    detectedIP,
	}

	// Pre-populate existing session files modTime so we don't blast notifications for existing old files
	initPattern := filepath.Join(cfg.SharedDir, "sessions", "*.json")
	if initFiles, err := filepath.Glob(initPattern); err == nil {
		for _, f := range initFiles {
			if fi, err := os.Stat(f); err == nil {
				s.fileModTime[f] = fi.ModTime()
			}
		}
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
	// Try outbound UDP connection to determine primary interface with 200ms timeout
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := d.Dial("udp", "8.8.8.8:80")
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

var (
	cachedHostIPs   []string
	cachedHostIPsMu sync.Mutex
	cachedHostIPsAt time.Time
)

// getAllHostIPs returns all IPv4 addresses of this machine along with hostname and loopback (cached 10s).
func getAllHostIPs() []string {
	cachedHostIPsMu.Lock()
	defer cachedHostIPsMu.Unlock()
	if len(cachedHostIPs) > 0 && time.Since(cachedHostIPsAt) < 10*time.Second {
		return cachedHostIPs
	}

	ips := []string{"127.0.0.1", "localhost", "::1"}
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ip4 := ipNet.IP.To4(); ip4 != nil {
					ips = append(ips, ip4.String())
				}
			}
		}
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		ips = append(ips, hostname)
	}
	cachedHostIPs = ips
	cachedHostIPsAt = time.Now()
	return ips
}

// isLocalHostIP checks if targetIP matches detectedIP or any of this host machine's interface IPs or hostname.
func isLocalHostIP(targetIP string, detectedIP string) bool {
	if ipMatches(targetIP, detectedIP) {
		return true
	}
	for _, ip := range getAllHostIPs() {
		if strings.EqualFold(ip, targetIP) {
			return true
		}
	}
	return false
}

// safeWriteFile writes data to a file safely using atomic temporary file rename.
func safeWriteFile(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return os.WriteFile(filePath, data, 0644)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return os.WriteFile(filePath, data, 0644)
	}
	return nil
}

// getUsersPaths returns the shared users.json path and local users.json path (next to exe).
func (s *AgentServer) getUsersPaths() (sharedPath string, localPath string) {
	sharedPath = filepath.Join(s.config.SharedDir, "users.json")
	if exePath, err := os.Executable(); err == nil {
		localPath = filepath.Join(filepath.Dir(exePath), "users.json")
	} else {
		localPath = filepath.Join(".", "users.json")
	}
	return filepath.Clean(sharedPath), filepath.Clean(localPath)
}

// syncUsersFilesLocked synchronizes users.json between local exeDir and remote sharedDir based on mtime.
func (s *AgentServer) syncUsersFilesLocked() {
	sharedPath, localPath := s.getUsersPaths()
	if sharedPath == localPath {
		return
	}

	fiShared, errS := os.Stat(sharedPath)
	fiLocal, errL := os.Stat(localPath)

	// 1. If shared doesn't exist, but local exists: copy local to shared
	if os.IsNotExist(errS) && errL == nil {
		if data, err := os.ReadFile(localPath); err == nil {
			var uf UsersFile
			if jsonErr := json.Unmarshal(data, &uf); jsonErr == nil && len(uf.Users) > 0 {
				_ = safeWriteFile(sharedPath, data)
				log.Printf("[USERS 동기화] 로컬 users.json(%s)을 공유폴더(%s)로 복사했습니다.\n", localPath, sharedPath)
			}
		}
		return
	}

	// 2. If local doesn't exist, but shared exists: copy shared to local
	if os.IsNotExist(errL) && errS == nil {
		if data, err := os.ReadFile(sharedPath); err == nil {
			var uf UsersFile
			if jsonErr := json.Unmarshal(data, &uf); jsonErr == nil && len(uf.Users) > 0 {
				_ = safeWriteFile(localPath, data)
				log.Printf("[USERS 동기화] 공유폴더 users.json(%s)을 로컬(%s)로 복사했습니다.\n", sharedPath, localPath)
			}
		}
		return
	}

	// 3. Both exist: compare modification times
	if errS == nil && errL == nil {
		// If local is newer than shared by at least 1 second
		if fiLocal.ModTime().After(fiShared.ModTime().Add(1 * time.Second)) {
			if dataL, err := os.ReadFile(localPath); err == nil {
				var uf UsersFile
				if jsonErr := json.Unmarshal(dataL, &uf); jsonErr == nil && len(uf.Users) > 0 {
					_ = safeWriteFile(sharedPath, dataL)
					log.Printf("[USERS 동기화] 로컬 users.json(%s)이 공유폴더보다 최신이어서 공유폴더(%s)로 자동 동기화했습니다.\n", localPath, sharedPath)
				} else if jsonErr != nil {
					log.Printf("[USERS 경고] 로컬 users.json 문법 오류로 공유폴더 덮어쓰기를 건너뜁니다: %v\n", jsonErr)
				}
			}
		} else if fiShared.ModTime().After(fiLocal.ModTime().Add(1 * time.Second)) {
			// Shared is newer than local
			if dataS, err := os.ReadFile(sharedPath); err == nil {
				var uf UsersFile
				if jsonErr := json.Unmarshal(dataS, &uf); jsonErr == nil && len(uf.Users) > 0 {
					_ = safeWriteFile(localPath, dataS)
					log.Printf("[USERS 동기화] 공유폴더 users.json(%s)이 로컬보다 최신이어서 로컬(%s)로 자동 동기화했습니다.\n", sharedPath, localPath)
				}
			}
		}
	}
}

// ensureUsersFile creates or verifies users.json in sharedDir and syncs with local.
func (s *AgentServer) ensureUsersFile() error {
	s.usersMutex.Lock()
	defer s.usersMutex.Unlock()

	s.syncUsersFilesLocked()

	sharedPath, localPath := s.getUsersPaths()
	if _, err := os.Stat(sharedPath); os.IsNotExist(err) {
		// Fallback from config/users.json if exists in current working dir
		fallbackPath := filepath.Join("config", "users.json")
		if data, err := os.ReadFile(fallbackPath); err == nil {
			var uf UsersFile
			if jsonErr := json.Unmarshal(data, &uf); jsonErr == nil && len(uf.Users) > 0 {
				_ = safeWriteFile(sharedPath, data)
				if sharedPath != localPath {
					_ = safeWriteFile(localPath, data)
				}
				s.usersSource = sharedPath
				return nil
			}
		}

		// Default users if nothing exists
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
		_ = safeWriteFile(sharedPath, data)
		if sharedPath != localPath {
			_ = safeWriteFile(localPath, data)
		}
	}
	s.usersSource = sharedPath
	return nil
}

// readUsers reads users.json thread-safely with automatic local/shared sync and robust fallback.
func (s *AgentServer) readUsers() (UsersFile, error) {
	s.usersMutex.Lock()
	if time.Since(s.lastUsersSync) > 1*time.Second {
		s.syncUsersFilesLocked()
		s.lastUsersSync = time.Now()
	}
	sharedPath, localPath := s.getUsersPaths()
	s.usersMutex.Unlock()

	s.usersMutex.RLock()
	defer s.usersMutex.RUnlock()

	// 1. Try reading shared users.json
	data, err := os.ReadFile(sharedPath)
	if err == nil {
		var uf UsersFile
		if jsonErr := json.Unmarshal(data, &uf); jsonErr == nil {
			s.usersSource = sharedPath
			return uf, nil
		} else {
			log.Printf("[USERS 에러] 공유폴더 users.json(%s) 문법 오류: %v\n", sharedPath, jsonErr)
			err = fmt.Errorf("공유폴더 users.json 문법 오류 (%v)", jsonErr)
		}
	}

	// 2. If sharedDir failed or unmarshal failed, try local users.json next to exe
	if localPath != "" && localPath != sharedPath {
		if localData, lErr := os.ReadFile(localPath); lErr == nil {
			var uf UsersFile
			if jsonErr := json.Unmarshal(localData, &uf); jsonErr == nil {
				s.usersSource = localPath
				log.Printf("[USERS 안내] 공유폴더 대신 로컬 users.json(%s)을 사용합니다.\n", localPath)
				return uf, nil
			} else {
				log.Printf("[USERS 에러] 로컬 users.json(%s) 문법 오류: %v\n", localPath, jsonErr)
				return UsersFile{}, fmt.Errorf("로컬 users.json 문법 오류 (%v)", jsonErr)
			}
		}
	}

	if err != nil {
		return UsersFile{}, err
	}
	return UsersFile{}, fmt.Errorf("users.json 파일을 찾을 수 없습니다: %s", sharedPath)
}

// writeUsers writes users.json thread-safely to both shared and local paths.
func (s *AgentServer) writeUsers(uf UsersFile) error {
	s.usersMutex.Lock()
	defer s.usersMutex.Unlock()

	sharedPath, localPath := s.getUsersPaths()
	uf.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(uf, "", "  ")
	if err != nil {
		return err
	}

	if err := safeWriteFile(sharedPath, data); err != nil {
		return err
	}
	if sharedPath != localPath {
		_ = safeWriteFile(localPath, data)
	}
	s.usersSource = sharedPath
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
func (s *AgentServer) postPeerJSON(url string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[PEER] JSON marshal error for %s: %v\n", url, err)
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		log.Printf("[PEER] NewRequest error for %s: %v\n", url, err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[PEER] Failed to deliver peer notification to %s: %v\n", url, err)
		return err
	}
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	log.Printf("[PEER] Successfully delivered peer notification to %s\n", url)
	return nil
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
		s.fileModTime[file] = modTime
		s.modMu.Unlock()

		if !exists {
			// A new session file appeared in sharedDir!
			if data, err := os.ReadFile(file); err == nil {
				var sess Session
				if err := json.Unmarshal(data, &sess); err == nil {
					log.Printf("[POLL] New session detected in shared folder: %s (Worker: %s, Checker: %s, Status: %s)\n", sess.SessionId, sess.Worker.Name, sess.Checker.Name, sess.Status)
					s.broadcastEvent("NEW_SESSION", sess.SessionId, sess)
				}
			}
		} else if modTime.After(prevTime) {
			// Existing file updated in sharedDir!
			if data, err := os.ReadFile(file); err == nil {
				var sess Session
				if err := json.Unmarshal(data, &sess); err == nil {
					log.Printf("[POLL] Session updated in shared folder: %s (Status: %s)\n", sess.SessionId, sess.Status)
					if sess.Status == "APPROVED" || sess.Status == "REJECTED" {
						s.broadcastEvent("SESSION_DECIDED", sess.SessionId, sess)
					} else {
						s.broadcastEvent("SESSION_REASSIGNED", sess.SessionId, sess)
					}
				}
			}
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
	mux.HandleFunc("/api/awx/config", s.handleAwxConfig)
	mux.HandleFunc("/api/awx/test", s.handleAwxTest)
	mux.HandleFunc("/api/awx/open-browser", s.handleAwxOpenBrowser)
	mux.HandleFunc("/api/awx/bridge/state", s.handleAwxBridgeState)
	mux.HandleFunc("/api/awx/bridge/collect", s.handleAwxBridgeCollect)
	mux.HandleFunc("/api/awx/bridge/result", s.handleAwxBridgeResult)
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
	mux.HandleFunc("/api/window/flash", s.handleWindowFlash)
	mux.HandleFunc("/api/window/restore", s.handleWindowRestore)
	mux.HandleFunc("/api/window/pin", s.handleWindowPin)
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
// clean shutdown when the user closes the window.
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
					// Case 1: Explicit /api/bye received on window unload and no reload ping arrived within 1.5s
					if bye && time.Since(byeAt) >= 1500*time.Millisecond {
						if onShutdown != nil {
							onShutdown()
						}
						return
					}
					// Case 2: Browser window closed abruptly without beacon (no ping for > 15s)
					// Protect startup with a 30s grace period from server launch
					if !bye && time.Since(startTime) > 30*time.Second && time.Since(lastPing) >= 15*time.Second {
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

var (
	reAwxInvURL           = regexp.MustCompile(`(?i)/inventories/(smart_inventory|inventory)/(\d+)`)
	reAwxApiInvURL        = regexp.MustCompile(`(?i)/inventories/(\d+)/(?:hosts|all_hosts)`)
	reAwxGroupURL         = regexp.MustCompile(`(?i)/groups/(\d+)/(?:nested_hosts|all_hosts|hosts)`)
	reAwxHostsURL         = regexp.MustCompile(`(?i)/(?:hosts)(?:\b|\?)`)
	reAwxPageQ            = regexp.MustCompile(`(?:^|[?&])(?:[\w.]+\.)?page=(\d+)`)
	reAwxScheme           = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	reCSRFInputNameFirst  = regexp.MustCompile(`(?i)<input[^>]*name=["']csrfmiddlewaretoken["'][^>]*value=["']([^"']+)["']`)
	reCSRFInputValueFirst = regexp.MustCompile(`(?i)<input[^>]*value=["']([^"']+)["'][^>]*name=["']csrfmiddlewaretoken["']`)
	reHTMLTitle           = regexp.MustCompile(`(?is)<title[^>]*>([^<]+)</title>`)
	rePasswordInput       = regexp.MustCompile(`(?i)<input[^>]*type=["']password["']`)
	reFormAction          = regexp.MustCompile(`(?is)<form[^>]*action=["']([^"']+)["']`)
)

// parseAwxTargetInputEx extracts origin baseURL, scope, page, and whether an explicit scheme was provided.
func parseAwxTargetInputEx(raw string) (baseURL string, scope string, page int, explicitScheme bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", 0, false
	}
	explicitScheme = reAwxScheme.MatchString(trimmed)
	candidate := trimmed
	if !explicitScheme {
		candidate = "https://" + strings.TrimLeft(trimmed, "/")
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(trimmed, "/"), "", 0, explicitScheme
	}
	baseURL = strings.ToLower(u.Scheme) + "://" + u.Host
	route := u.Path
	if u.Fragment != "" {
		route += " " + u.Fragment
	}
	if m := reAwxGroupURL.FindStringSubmatch(route); len(m) == 2 {
		scope = "group:" + m[1]
	} else if m := reAwxInvURL.FindStringSubmatch(route); len(m) == 3 {
		scope = strings.ToLower(m[1]) + ":" + m[2]
	} else if m := reAwxApiInvURL.FindStringSubmatch(route); len(m) == 2 {
		scope = "inventory:" + m[1]
	} else if reAwxHostsURL.MatchString(route) && !strings.Contains(strings.ToLower(route), "/inventories/") {
		scope = "all"
	}
	if idx := strings.Index(route, "?"); idx >= 0 {
		if pm := reAwxPageQ.FindStringSubmatch(route[idx+1:]); len(pm) == 2 {
			if p, pErr := strconv.Atoi(pm[1]); pErr == nil && p > 0 {
				page = p
			}
		}
	}
	if page == 0 && u.RawQuery != "" {
		if pm := reAwxPageQ.FindStringSubmatch(u.RawQuery); len(pm) == 2 {
			if p, pErr := strconv.Atoi(pm[1]); pErr == nil && p > 0 {
				page = p
			}
		}
	}
	return baseURL, scope, page, explicitScheme
}

// parseAwxTargetInput extracts the origin base URL, scope string, and optional page from a pasted AWX URL.
func parseAwxTargetInput(raw string) (baseURL string, scope string, page int) {
	b, sc, pg, _ := parseAwxTargetInputEx(raw)
	return b, sc, pg
}

func buildAwxConsoleURL(baseURL, defaultScope, rawURL string) string {
	trimmedRaw := strings.TrimSpace(rawURL)
	if trimmedRaw != "" && (strings.Contains(trimmedRaw, "#/") || strings.Contains(trimmedRaw, "/inventories/") || strings.Contains(trimmedRaw, "/groups/")) {
		if !reAwxScheme.MatchString(trimmedRaw) {
			return "https://" + strings.TrimLeft(trimmedRaw, "/")
		}
		return trimmedRaw
	}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		return ""
	}
	if !reAwxScheme.MatchString(base) {
		base = "https://" + strings.TrimLeft(base, "/")
	}
	scopeType, scopeID, _ := normalizeAwxScope(defaultScope, "3")
	switch scopeType {
	case "all":
		return base + "/#/hosts"
	case "group":
		return fmt.Sprintf("%s/#/groups/%s/nested_hosts", base, scopeID)
	case "smart_inventory":
		return fmt.Sprintf("%s/#/inventories/smart_inventory/%s/hosts", base, scopeID)
	default:
		return fmt.Sprintf("%s/#/inventories/inventory/%s/hosts", base, scopeID)
	}
}

func probeServerSPKIHashes(rawURL string, timeout time.Duration) []string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return nil
	}
	addr := u.Host
	if _, _, splitErr := net.SplitHostPort(addr); splitErr != nil {
		addr = net.JoinHostPort(u.Hostname(), "443")
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS10,
		ServerName:         u.Hostname(),
	})
	if err != nil {
		return nil
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	seen := make(map[string]struct{}, len(certs))
	hashes := make([]string, 0, len(certs))
	for _, cert := range certs {
		if cert == nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
			continue
		}
		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		b64 := base64.StdEncoding.EncodeToString(sum[:])
		if _, ok := seen[b64]; !ok {
			seen[b64] = struct{}{}
			hashes = append(hashes, b64)
		}
	}
	return hashes
}

func isExplicitScope(scope string) bool {
	s := strings.TrimSpace(scope)
	return s != "" && s != "3" && s != "inventory:3"
}

// normalizeAwxScope canonicalizes inventory/group/all specifiers into scopeType, scopeID, and normalized key.
func normalizeAwxScope(raw string, fallback string) (scopeType string, scopeID string, normKey string) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(fallback))
	}
	if s == "" {
		s = "3"
	}
	if s == "all" || s == "0" {
		return "all", "all", "all"
	}
	if strings.HasPrefix(s, "group:") {
		id := strings.TrimSpace(strings.TrimPrefix(s, "group:"))
		if id != "" {
			return "group", id, "group:" + id
		}
	}
	if strings.HasPrefix(s, "smart_inventory:") {
		id := strings.TrimSpace(strings.TrimPrefix(s, "smart_inventory:"))
		if id != "" {
			return "smart_inventory", id, "smart_inventory:" + id
		}
	}
	if strings.HasPrefix(s, "inventory:") {
		id := strings.TrimSpace(strings.TrimPrefix(s, "inventory:"))
		if id != "" {
			return "inventory", id, "inventory:" + id
		}
	}
	return "inventory", s, "inventory:" + s
}

func buildAwxEndpoint(baseURL, scopeType, scopeID string, pageSize int) string {
	base := strings.TrimRight(baseURL, "/")
	switch scopeType {
	case "all":
		return fmt.Sprintf("%s/api/v2/hosts/?page_size=%d", base, pageSize)
	case "group":
		return fmt.Sprintf("%s/api/v2/groups/%s/all_hosts/?page_size=%d", base, scopeID, pageSize)
	default:
		return fmt.Sprintf("%s/api/v2/inventories/%s/hosts/?page_size=%d", base, scopeID, pageSize)
	}
}

func (s *AgentServer) getAwxSnapshot() (baseURL, username, password, token, defaultScope string, insecureTLS bool, client *http.Client) {
	s.awxCfgMu.RLock()
	defer s.awxCfgMu.RUnlock()
	insecure := true
	if s.config.AwxInsecureTLS != nil {
		insecure = *s.config.AwxInsecureTLS
	}
	return s.config.AwxBaseURL, s.config.AwxUsername, s.config.AwxPassword, s.config.AwxToken, s.config.AwxDefaultScope, insecure, s.awxHttpClient
}

func awxSessionCacheKey(baseURL, username, password string, insecureTLS bool) string {
	h := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%s|%s|%x|%v", strings.ToLower(strings.TrimRight(baseURL, "/")), username, h[:8], insecureTLS)
}

func (s *AgentServer) clearAwxSessionCache() {
	s.awxSessionMu.Lock()
	s.awxSessionKey = ""
	s.awxSessionClient = nil
	s.awxSessionMu.Unlock()
}

func (s *AgentServer) clearAwxSessionCacheKey(key string) {
	s.awxSessionMu.Lock()
	if s.awxSessionKey == key {
		s.awxSessionKey = ""
		s.awxSessionClient = nil
	}
	s.awxSessionMu.Unlock()
}

func (s *AgentServer) getCachedAwxSession(key string) *http.Client {
	s.awxSessionMu.Lock()
	defer s.awxSessionMu.Unlock()
	if s.awxSessionKey == key && s.awxSessionClient != nil {
		return s.awxSessionClient
	}
	return nil
}

func (s *AgentServer) setCachedAwxSession(key string, client *http.Client) {
	s.awxSessionMu.Lock()
	s.awxSessionKey = key
	s.awxSessionClient = client
	s.awxSessionMu.Unlock()
}

func extractCookieValue(jar http.CookieJar, rawURL, name string) string {
	if jar == nil {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if strings.EqualFold(c.Name, name) && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func extractCsrfFromHTML(htmlStr string) string {
	if m := reCSRFInputNameFirst.FindStringSubmatch(htmlStr); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := reCSRFInputValueFirst.FindStringSubmatch(htmlStr); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func looksLikeHTML(contentType, snippet string) bool {
	ct := strings.ToLower(contentType)
	trim := strings.TrimSpace(snippet)
	return strings.Contains(ct, "text/html") || strings.HasPrefix(trim, "<")
}

func looksLikeSSOHtml(origURL, finalURL, htmlStr string) bool {
	lu := strings.ToLower(finalURL)
	ou, _ := url.Parse(origURL)
	fu, _ := url.Parse(finalURL)
	if ou != nil && fu != nil && fu.Hostname() != "" && !strings.EqualFold(ou.Hostname(), fu.Hostname()) && net.ParseIP(ou.Hostname()) == nil {
		if !rePasswordInput.MatchString(htmlStr) {
			return true
		}
	}
	for _, marker := range []string{"/saml", "/oidc", "/oauth", "keycloak", "adfs", "/sso/"} {
		if strings.Contains(lu, marker) {
			return true
		}
	}
	lowHTML := strings.ToLower(htmlStr)
	if !rePasswordInput.MatchString(htmlStr) && (strings.Contains(lowHTML, "samlrequest") || strings.Contains(lowHTML, "single sign-on")) {
		return true
	}
	return false
}

func formatAwxHTTPError(status int, contentType, origURL, finalURL, snippet string) error {
	trim := strings.TrimSpace(snippet)
	var apiErr struct {
		Detail string `json:"detail"`
	}
	if strings.HasPrefix(trim, "{") && json.Unmarshal([]byte(trim), &apiErr) == nil && strings.TrimSpace(apiErr.Detail) != "" {
		return fmt.Errorf("HTTP %d (%s)", status, strings.TrimSpace(apiErr.Detail))
	}
	if looksLikeHTML(contentType, trim) {
		if looksLikeSSOHtml(origURL, finalURL, trim) {
			return fmt.Errorf("SSO(통합인증) 페이지 응답 (%s) - 'AWX 창 열기'로 로그인 후 'AWX 탭 연결' 북마크릿 또는 Bearer Token 사용 권장", finalURL)
		}
		if strings.Contains(strings.ToLower(trim), "csrf") {
			return fmt.Errorf("HTTP %d CSRF 검증 실패 (hosts 등록 DNS 예: https://awxhost 사용 또는 Bearer Token 권장)", status)
		}
		title := ""
		if m := reHTMLTitle.FindStringSubmatch(trim); len(m) == 2 {
			title = strings.TrimSpace(m[1])
		}
		if title != "" {
			return fmt.Errorf("JSON 대신 HTML 반환 (HTTP %d, 제목: %s)", status, title)
		}
		return fmt.Errorf("JSON 대신 HTML 반환 (HTTP %d, URL: %s)", status, finalURL)
	}
	if status != http.StatusOK {
		return fmt.Errorf("AWX API returned HTTP %d", status)
	}
	return fmt.Errorf("AWX API 응답 파싱 실패")
}

func doAwxGetJSON(pageURL, authHeader string, client *http.Client) (*awxPageResponse, int, string, string, string, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, 0, "", pageURL, "", err
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("User-Agent", awxRequestUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", pageURL, "", err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	finalURL := pageURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, resp.StatusCode, ct, finalURL, "", readErr
	}
	snippet := string(rawBody)
	if len(snippet) > 4096 {
		snippet = snippet[:4096]
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, ct, finalURL, snippet, formatAwxHTTPError(resp.StatusCode, ct, pageURL, finalURL, snippet)
	}
	if looksLikeHTML(ct, snippet) {
		return nil, resp.StatusCode, ct, finalURL, snippet, formatAwxHTTPError(resp.StatusCode, ct, pageURL, finalURL, snippet)
	}
	var parsed awxPageResponse
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return nil, resp.StatusCode, ct, finalURL, snippet, fmt.Errorf("JSON 파싱 오류: %w", err)
	}
	return &parsed, resp.StatusCode, ct, finalURL, snippet, nil
}

func tryLoginAwxSession(baseOrigin, username, password string, insecureTLS bool) (*http.Client, string, error) {
	sessionClient := newAwxHTTPClient(insecureTLS)
	loginCandidates := []string{
		strings.TrimRight(baseOrigin, "/") + "/api/login/",
		strings.TrimRight(baseOrigin, "/") + "/api/gateway/v1/login/",
		strings.TrimRight(baseOrigin, "/") + "/login/",
	}

	var lastErr error
	saw404Only := true
	for _, loginPageURL := range loginCandidates {
		getReq, err := http.NewRequest(http.MethodGet, loginPageURL, nil)
		if err != nil {
			continue
		}
		getReq.Header.Set("User-Agent", awxRequestUserAgent)
		getReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
		getResp, err := sessionClient.Do(getReq)
		if err != nil {
			lastErr = err
			saw404Only = false
			continue
		}
		htmlBytes, _ := io.ReadAll(io.LimitReader(getResp.Body, 256*1024))
		getResp.Body.Close()
		if getResp.StatusCode == http.StatusNotFound || getResp.StatusCode == http.StatusMethodNotAllowed {
			continue
		}
		saw404Only = false
		htmlStr := string(htmlBytes)
		finalGetURL := loginPageURL
		if getResp.Request != nil && getResp.Request.URL != nil {
			finalGetURL = getResp.Request.URL.String()
		}
		if looksLikeSSOHtml(loginPageURL, finalGetURL, htmlStr) {
			return nil, "", fmt.Errorf("SSO(통합인증) 페이지로 이동됨 (%s) - 'AWX 창 열기'로 로그인 후 'AWX 탭 연결' 북마크릿 또는 Bearer Token을 사용하세요", finalGetURL)
		}

		postTarget := finalGetURL
		if m := reFormAction.FindStringSubmatch(htmlStr); len(m) == 2 {
			act := strings.TrimSpace(m[1])
			if act != "" && !strings.HasPrefix(strings.ToLower(act), "javascript:") {
				if baseU, pErr := url.Parse(finalGetURL); pErr == nil {
					if relU, rErr := baseU.Parse(act); rErr == nil {
						if !strings.Contains(strings.ToLower(relU.Path), "sso") {
							postTarget = relU.String()
						}
					}
				}
			}
		}

		csrfForm := extractCsrfFromHTML(htmlStr)
		csrfCookie := extractCookieValue(sessionClient.Jar, finalGetURL, "csrftoken")
		if csrfCookie == "" {
			csrfCookie = extractCookieValue(sessionClient.Jar, baseOrigin+"/", "csrftoken")
		}
		if csrfForm == "" && csrfCookie == "" {
			if tokReq, tErr := http.NewRequest(http.MethodGet, strings.TrimRight(baseOrigin, "/")+"/api/v2/", nil); tErr == nil {
				tokReq.Header.Set("User-Agent", awxRequestUserAgent)
				if tokResp, doErr := sessionClient.Do(tokReq); doErr == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(tokResp.Body, 4096))
					tokResp.Body.Close()
				}
				csrfCookie = extractCookieValue(sessionClient.Jar, baseOrigin+"/", "csrftoken")
			}
		}
		if csrfForm == "" {
			csrfForm = csrfCookie
		}

		formVals := url.Values{}
		formVals.Set("username", username)
		formVals.Set("password", password)
		formVals.Set("next", "/api/v2/")
		if csrfForm != "" {
			formVals.Set("csrfmiddlewaretoken", csrfForm)
		}
		postReq, err := http.NewRequest(http.MethodPost, postTarget, strings.NewReader(formVals.Encode()))
		if err != nil {
			continue
		}
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.Header.Set("User-Agent", awxRequestUserAgent)
		postReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
		postReq.Header.Set("Referer", finalGetURL)
		if parsedPost, pErr := url.Parse(postTarget); pErr == nil && parsedPost.Scheme != "" && parsedPost.Host != "" {
			postReq.Header.Set("Origin", parsedPost.Scheme+"://"+parsedPost.Host)
		}
		if csrfCookie != "" {
			postReq.Header.Set("X-CSRFToken", csrfCookie)
		} else if csrfForm != "" {
			postReq.Header.Set("X-CSRFToken", csrfForm)
		}

		postResp, err := sessionClient.Do(postReq)
		if err != nil {
			lastErr = err
			continue
		}
		postBytes, _ := io.ReadAll(io.LimitReader(postResp.Body, 128*1024))
		postResp.Body.Close()
		postBody := string(postBytes)
		lowPostBody := strings.ToLower(postBody)

		if postResp.StatusCode == http.StatusForbidden && strings.Contains(lowPostBody, "csrf") {
			return nil, "", fmt.Errorf("AWX CSRF 검증 실패 (hosts 등록 DNS 예: https://awxhost 로 접속하거나 Bearer Token 사용 권장)")
		}
		if strings.Contains(lowPostBody, "please enter a correct username and password") ||
			strings.Contains(lowPostBody, "invalid username or password") ||
			strings.Contains(lowPostBody, "unable to log in with provided credentials") {
			return nil, "", fmt.Errorf("AWX 로그인 실패 (계정 ID 또는 비밀번호가 올바르지 않습니다)")
		}
		return sessionClient, "", nil
	}

	// Fallback for legacy Ansible Tower (<3.3) /api/v2/authtoken/
	if saw404Only {
		payload, _ := json.Marshal(map[string]string{
			"username": username,
			"password": password,
		})
		atReq, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseOrigin, "/")+"/api/v2/authtoken/", bytes.NewReader(payload))
		if err == nil {
			atReq.Header.Set("Content-Type", "application/json")
			atReq.Header.Set("User-Agent", awxRequestUserAgent)
			atReq.Header.Set("Accept", "application/json")
			atResp, doErr := sessionClient.Do(atReq)
			if doErr == nil {
				atBody, _ := io.ReadAll(io.LimitReader(atResp.Body, 32*1024))
				atResp.Body.Close()
				if atResp.StatusCode == http.StatusOK {
					var tokRes struct {
						Token string `json:"token"`
					}
					if json.Unmarshal(atBody, &tokRes) == nil && strings.TrimSpace(tokRes.Token) != "" {
						return sessionClient, "Token " + strings.TrimSpace(tokRes.Token), nil
					}
				}
			}
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("AWX 세션 로그인 엔드포인트를 찾지 못했습니다")
}

func isAwxRetryableAuthOrHTML(status int, ct, snippet string) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || looksLikeHTML(ct, snippet)
}

func (s *AgentServer) fetchAwxPageWithDetails(pageURL, username, password, token string, insecureTLS bool, client *http.Client) (*awxPageResponse, string, string, error) {
	if client == nil {
		client = newAwxHTTPClient(insecureTLS)
	}
	tok := strings.TrimSpace(token)
	user := strings.TrimSpace(username)
	if tok == "" && user == "" {
		return nil, "", pageURL, fmt.Errorf("AWX 계정 ID/비밀번호 또는 Bearer Token을 입력해 주세요")
	}

	parsedU, _ := url.Parse(pageURL)
	baseOrigin := ""
	if parsedU != nil && parsedU.Scheme != "" && parsedU.Host != "" {
		baseOrigin = parsedU.Scheme + "://" + parsedU.Host
	}

	tryWithControllerFallback := func(targetURL, authHeader string, cl *http.Client) (*awxPageResponse, int, string, string, string, error) {
		pg, st, ct, finalURL, snip, err := doAwxGetJSON(targetURL, authHeader, cl)
		if err != nil && st == http.StatusNotFound && strings.Contains(targetURL, "/api/v2/") {
			altURL := strings.Replace(targetURL, "/api/v2/", "/api/controller/v2/", 1)
			pg2, st2, ct2, f2, sn2, err2 := doAwxGetJSON(altURL, authHeader, cl)
			if err2 == nil || st2 != http.StatusNotFound {
				return pg2, st2, ct2, f2, sn2, err2
			}
		}
		return pg, st, ct, finalURL, snip, err
	}

	if tok != "" {
		pg, st, _, finalURL, _, err := tryWithControllerFallback(pageURL, "Bearer "+tok, client)
		if err == nil {
			return pg, "bearer", finalURL, nil
		}
		if st == http.StatusUnauthorized && !strings.Contains(tok, " ") {
			pg2, _, _, f2, _, err2 := tryWithControllerFallback(pageURL, "Token "+tok, client)
			if err2 == nil {
				return pg2, "token", f2, nil
			}
			return nil, "", f2, err2
		}
		return nil, "", finalURL, err
	}

	sessionKey := awxSessionCacheKey(baseOrigin, user, password, insecureTLS)
	if password != "" {
		if cachedClient := s.getCachedAwxSession(sessionKey); cachedClient != nil {
			pg, _, _, finalURL, _, err := tryWithControllerFallback(pageURL, "", cachedClient)
			if err == nil {
				return pg, "session", finalURL, nil
			}
			s.clearAwxSessionCacheKey(sessionKey)
		}
	}

	basicHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	pg, st, ct, finalURL, snip, err := tryWithControllerFallback(pageURL, basicHeader, client)
	if err == nil {
		return pg, "basic", finalURL, nil
	}

	// If http:// redirected root/login to https:// while dropping /api/v2/..., promote to https:// target and retry
	effectivePageURL := pageURL
	effectiveOrigin := baseOrigin
	if fu, fErr := url.Parse(finalURL); fErr == nil && strings.EqualFold(fu.Scheme, "https") && strings.HasPrefix(strings.ToLower(pageURL), "http://") {
		if origU, oErr := url.Parse(pageURL); oErr == nil {
			rebuilt := *origU
			rebuilt.Scheme = "https"
			if fu.Host != "" {
				rebuilt.Host = fu.Host
			}
			effectivePageURL = rebuilt.String()
			effectiveOrigin = "https://" + rebuilt.Host
			if pgH, _, _, fH, _, errH := tryWithControllerFallback(effectivePageURL, basicHeader, client); errH == nil {
				return pgH, "basic", fH, nil
			}
		}
	}

	// Fallback to AWX web form cookie session login when Basic Auth returns 401/403/HTML
	if password != "" && isAwxRetryableAuthOrHTML(st, ct, snip) && effectiveOrigin != "" {
		sessClient, authTokHeader, loginErr := tryLoginAwxSession(effectiveOrigin, user, password, insecureTLS)
		if loginErr != nil {
			return nil, "", finalURL, loginErr
		}
		pgS, _, _, finalS, _, errS := tryWithControllerFallback(effectivePageURL, authTokHeader, sessClient)
		if errS == nil {
			if authTokHeader == "" {
				s.setCachedAwxSession(sessionKey, sessClient)
				if effectiveOrigin != baseOrigin && baseOrigin != "" {
					s.setCachedAwxSession(awxSessionCacheKey(effectiveOrigin, user, password, insecureTLS), sessClient)
				}
			}
			return pgS, "session", finalS, nil
		}
		return nil, "", finalS, errS
	}

	return nil, "", finalURL, err
}

func (s *AgentServer) fetchAwxPageWithCreds(pageURL, username, password, token string, client *http.Client) (*awxPageResponse, error) {
	insecure := true
	if client != nil {
		if tr, ok := client.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil {
			insecure = tr.TLSClientConfig.InsecureSkipVerify
		}
	}
	pg, _, _, err := s.fetchAwxPageWithDetails(pageURL, username, password, token, insecure, client)
	return pg, err
}

func (s *AgentServer) fetchAwxPage(pageURL string) (*awxPageResponse, error) {
	_, user, pass, tok, _, insecure, client := s.getAwxSnapshot()
	pg, _, _, err := s.fetchAwxPageWithDetails(pageURL, user, pass, tok, insecure, client)
	return pg, err
}

// fetchAwxInventoryHosts retrieves and caches all hosts for an AWX scope.
func (s *AgentServer) fetchAwxInventoryHosts(invID string, refresh bool) ([]string, error) {
	baseURL, user, pass, tok, defScope, insecure, client := s.getAwxSnapshot()
	scopeType, scopeID, normKey := normalizeAwxScope(invID, defScope)
	cacheKey := baseURL + "|" + normKey

	if !refresh {
		s.awxCacheMu.RLock()
		entry, ok := s.awxCache[cacheKey]
		s.awxCacheMu.RUnlock()
		if ok && time.Since(entry.fetchedAt) < 60*time.Second && len(entry.hosts) > 0 {
			cp := make([]string, len(entry.hosts))
			copy(cp, entry.hosts)
			return cp, nil
		}
	}

	basePath := buildAwxEndpoint(baseURL, scopeType, scopeID, 200)
	page1, _, resolvedPage1URL, err := s.fetchAwxPageWithDetails(basePath+"&page=1", user, pass, tok, insecure, client)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(resolvedPage1URL, "&page=1") {
		basePath = strings.TrimSuffix(resolvedPage1URL, "&page=1")
	}

	pageSize := len(page1.Results)
	totalPages := 1
	if page1.Next != nil && strings.TrimSpace(*page1.Next) != "" {
		if pageSize > 0 && page1.Count > pageSize {
			totalPages = (page1.Count + pageSize - 1) / pageSize
		} else {
			totalPages = (page1.Count + 199) / 200
			if totalPages < 2 {
				totalPages = 2
			}
		}
	} else if page1.Count > 200 && (pageSize == 0 || page1.Count > pageSize) {
		eff := 200
		if pageSize > 0 {
			eff = pageSize
		}
		totalPages = (page1.Count + eff - 1) / eff
	}
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
	s.awxCache[cacheKey] = awxCacheEntry{
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

// filterHostList applies pipe-delimited exclusion rules consistently for API and browser bridge responses.
func filterHostList(rawHosts []string, excludePattern string) (filtered []string, excluded []string, results []map[string]string) {
	var filters []string
	if excludePattern != "" {
		for _, part := range strings.Split(excludePattern, "|") {
			f := strings.ToLower(strings.TrimSpace(part))
			if f != "" {
				filters = append(filters, f)
			}
		}
	}

	filtered = make([]string, 0, len(rawHosts))
	excluded = make([]string, 0)
	results = make([]map[string]string, 0, len(rawHosts))

	seen := make(map[string]struct{}, len(rawHosts))
	for _, raw := range rawHosts {
		h := strings.ToLower(strings.TrimSpace(raw))
		if h == "" {
			continue
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		results = append(results, map[string]string{"name": h})

		isExcluded := false
		for _, f := range filters {
			if h == f || strings.Contains(h, f) {
				isExcluded = true
				break
			}
		}
		if isExcluded {
			excluded = append(excluded, h)
		} else {
			filtered = append(filtered, h)
		}
	}
	return filtered, excluded, results
}

// handleAwxHosts: GET /api/awx/hosts?inventory=3&scope=...&page=...&limit=...&exclude=...
func (s *AgentServer) handleAwxHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	rawScope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if rawScope == "" {
		rawScope = strings.TrimSpace(r.URL.Query().Get("inventory"))
	}
	baseURL, _, _, _, defScope, _, _ := s.getAwxSnapshot()
	scopeType, scopeID, normKey := normalizeAwxScope(rawScope, defScope)

	refresh := r.URL.Query().Get("refresh") == "true"
	excludePattern := strings.TrimSpace(r.URL.Query().Get("exclude"))
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
	pageStr := strings.TrimSpace(r.URL.Query().Get("page"))

	var rawHosts []string
	if pageStr != "" {
		pageNum, pErr := strconv.Atoi(pageStr)
		if pErr != nil || pageNum < 1 {
			pageNum = 1
		}
		pageSize := 20
		if limitStr != "" {
			if l, lErr := strconv.Atoi(limitStr); lErr == nil && l > 0 {
				pageSize = l
			}
		}
		pg, err := s.fetchAwxPage(fmt.Sprintf("%s&page=%d", buildAwxEndpoint(baseURL, scopeType, scopeID, pageSize), pageNum))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("AWX 호스트 수집 실패: %v", err)})
			return
		}
		for _, item := range pg.Results {
			h := strings.ToLower(strings.TrimSpace(item.Name))
			if h != "" {
				rawHosts = append(rawHosts, h)
			}
		}
	} else {
		all, err := s.fetchAwxInventoryHosts(normKey, refresh)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("AWX 호스트 수집 실패: %v", err)})
			return
		}
		rawHosts = all
		if limitStr != "" {
			if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit < len(rawHosts) {
				rawHosts = rawHosts[:limit]
			}
		}
	}

	filteredHosts, excludedHosts, results := filterHostList(rawHosts, excludePattern)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":         len(filteredHosts),
		"rawCount":      len(rawHosts),
		"next":          nil,
		"inventoryId":   scopeID,
		"scopeType":     scopeType,
		"scopeKey":      normKey,
		"hosts":         filteredHosts,
		"excludedHosts": excludedHosts,
		"results":       results,
	})
}

func (s *AgentServer) getBridgeSnapshot() map[string]interface{} {
	s.bridgeMu.RLock()
	st := s.bridgeState
	s.bridgeMu.RUnlock()
	cnt := 0
	if s.wsHub != nil {
		cnt = s.wsHub.BridgeCount()
	}
	connected := cnt > 0 || (st.Connected && !st.UpdatedAt.IsZero() && time.Since(st.UpdatedAt) < 2*time.Minute)
	return map[string]interface{}{
		"connected":   connected,
		"bridgeCount": cnt,
		"url":         st.URL,
		"origin":      st.Origin,
		"hash":        st.Hash,
		"label":       st.Label,
		"scopeType":   st.ScopeType,
		"scopeId":     st.ScopeID,
		"page":        st.Page,
		"pageCount":   len(st.PageHosts),
		"updatedAt":   st.UpdatedAt,
	}
}

// handleAwxConfig: GET/POST /api/awx/config
func (s *AgentServer) handleAwxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		baseURL, user, pass, tok, defScope, insecure, _ := s.getAwxSnapshot()
		_, _, normKey := normalizeAwxScope(defScope, "3")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"baseUrl":      baseURL,
			"username":     user,
			"hasPassword":  pass != "",
			"hasToken":     tok != "",
			"defaultScope": normKey,
			"insecureTls":  insecure,
			"bridge":       s.getBridgeSnapshot(),
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	var req struct {
		BaseURL      *string `json:"baseUrl"`
		DefaultScope *string `json:"defaultScope"`
		Username     *string `json:"username"`
		Password     *string `json:"password"`
		Token        *string `json:"token"`
		InsecureTLS  *bool   `json:"insecureTls"`
		ClearAuth    bool    `json:"clearAuth"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	s.awxCfgMu.Lock()
	var parsedScope string
	serverChanged := false
	prevBase := strings.TrimRight(s.config.AwxBaseURL, "/")
	prevWasFactoryDefault := strings.EqualFold(prevBase, "http://192.168.0.58:30080") &&
		s.config.AwxUsername == "test.kim" && s.config.AwxPassword == "11111111!"
	if req.BaseURL != nil {
		b, sc, _ := parseAwxTargetInput(*req.BaseURL)
		if b != "" {
			if !strings.EqualFold(b, prevBase) {
				serverChanged = true
			}
			s.config.AwxBaseURL = b
		}
		parsedScope = sc
	}
	if req.DefaultScope != nil && strings.TrimSpace(*req.DefaultScope) != "" {
		s.config.AwxDefaultScope = strings.TrimSpace(*req.DefaultScope)
	} else if parsedScope != "" {
		s.config.AwxDefaultScope = parsedScope
	}
	if req.ClearAuth || (serverChanged && prevWasFactoryDefault) {
		s.config.AwxUsername = ""
		s.config.AwxPassword = ""
		s.config.AwxToken = ""
	}
	if req.Username != nil {
		s.config.AwxUsername = strings.TrimSpace(*req.Username)
	}
	if req.Password != nil && *req.Password != "" {
		s.config.AwxPassword = *req.Password
	}
	if req.Token != nil {
		// Explicit token field updates only when non-empty unless ClearAuth is set
		if strings.TrimSpace(*req.Token) != "" {
			s.config.AwxToken = strings.TrimSpace(*req.Token)
		}
	}
	if req.InsecureTLS != nil {
		val := *req.InsecureTLS
		s.config.AwxInsecureTLS = &val
		s.awxHttpClient = newAwxHTTPClient(val)
	}
	baseURL := s.config.AwxBaseURL
	user := s.config.AwxUsername
	hasPass := s.config.AwxPassword != ""
	hasTok := s.config.AwxToken != ""
	defScope := s.config.AwxDefaultScope
	insecure := s.config.AwxInsecureTLS == nil || *s.config.AwxInsecureTLS
	s.awxCfgMu.Unlock()

	s.clearAwxSessionCache()
	s.awxCacheMu.Lock()
	s.awxCache = make(map[string]awxCacheEntry)
	s.awxCacheMu.Unlock()

	_, _, normKey := normalizeAwxScope(defScope, "3")
	respPayload := map[string]interface{}{
		"ok":           true,
		"baseUrl":      baseURL,
		"username":     user,
		"hasPassword":  hasPass,
		"hasToken":     hasTok,
		"defaultScope": normKey,
		"insecureTls":  insecure,
	}
	s.broadcastEvent("AWX_CONFIG_UPDATED", "", respPayload)
	writeJSON(w, http.StatusOK, respPayload)
}

func isAwxTransportOrTLSError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"server gave http response to https client",
		"tls:",
		"handshake",
		"connection refused",
		"unexpected eof",
		"eof",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// handleAwxTest: POST /api/awx/test verifies AWX connectivity with submitted or current config.
func (s *AgentServer) handleAwxTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	baseURL, user, pass, tok, defScope, insecure, _ := s.getAwxSnapshot()

	var req struct {
		BaseURL             string `json:"baseUrl"`
		RawURL              string `json:"rawUrl"`
		DefaultScope        string `json:"defaultScope"`
		Username            string `json:"username"`
		Password            string `json:"password"`
		Token               string `json:"token"`
		InsecureTLS         *bool  `json:"insecureTls"`
		AllowSchemeFallback *bool  `json:"allowSchemeFallback"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	explicitScheme := true
	urlCandidate := strings.TrimSpace(req.RawURL)
	if urlCandidate == "" {
		urlCandidate = strings.TrimSpace(req.BaseURL)
	}
	if urlCandidate != "" {
		b, sc, _, exp := parseAwxTargetInputEx(urlCandidate)
		if b != "" {
			baseURL = b
		}
		explicitScheme = exp
		if req.DefaultScope == "" && sc != "" {
			defScope = sc
		}
	}
	if req.AllowSchemeFallback != nil {
		explicitScheme = !*req.AllowSchemeFallback
	}
	if strings.TrimSpace(req.DefaultScope) != "" {
		defScope = strings.TrimSpace(req.DefaultScope)
	}
	if strings.TrimSpace(req.Username) != "" {
		user = strings.TrimSpace(req.Username)
	}
	if req.Password != "" {
		pass = req.Password
	}
	if strings.TrimSpace(req.Token) != "" {
		tok = strings.TrimSpace(req.Token)
	}
	if req.InsecureTLS != nil {
		insecure = *req.InsecureTLS
	}

	scopeType, scopeID, normKey := normalizeAwxScope(defScope, "3")
	testURL := buildAwxEndpoint(baseURL, scopeType, scopeID, 5) + "&page=1"
	client := newAwxHTTPClient(insecure)
	pg, authMethod, resolvedURL, err := s.fetchAwxPageWithDetails(testURL, user, pass, tok, insecure, client)
	if err != nil && !explicitScheme && isAwxTransportOrTLSError(err) {
		altBase := ""
		if strings.HasPrefix(strings.ToLower(baseURL), "https://") {
			altBase = "http://" + baseURL[8:]
		} else if strings.HasPrefix(strings.ToLower(baseURL), "http://") {
			altBase = "https://" + baseURL[7:]
		}
		if altBase != "" {
			altTestURL := buildAwxEndpoint(altBase, scopeType, scopeID, 5) + "&page=1"
			if pgAlt, authAlt, resAlt, errAlt := s.fetchAwxPageWithDetails(altTestURL, user, pass, tok, insecure, newAwxHTTPClient(insecure)); errAlt == nil {
				pg = pgAlt
				authMethod = authAlt
				resolvedURL = resAlt
				baseURL = altBase
				testURL = altTestURL
				err = nil
			}
		}
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"ok":    false,
			"url":   testURL,
			"error": fmt.Sprintf("AWX 연결 실패: %v", err),
		})
		return
	}

	effectiveBaseURL := baseURL
	if ru, rErr := url.Parse(resolvedURL); rErr == nil && ru.Scheme != "" && ru.Host != "" {
		effectiveBaseURL = strings.ToLower(ru.Scheme) + "://" + ru.Host
	}
	if effectiveBaseURL != "" && strings.TrimSpace(req.BaseURL) == "" {
		s.awxCfgMu.Lock()
		changed := !strings.EqualFold(effectiveBaseURL, strings.TrimRight(s.config.AwxBaseURL, "/"))
		if changed {
			s.config.AwxBaseURL = effectiveBaseURL
		}
		s.awxCfgMu.Unlock()
		if changed {
			s.awxCacheMu.Lock()
			s.awxCache = make(map[string]awxCacheEntry)
			s.awxCacheMu.Unlock()
		}
	}

	samples := make([]string, 0, len(pg.Results))
	for _, it := range pg.Results {
		if n := strings.TrimSpace(it.Name); n != "" {
			samples = append(samples, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"url":          testURL,
		"baseUrl":      effectiveBaseURL,
		"defaultScope": normKey,
		"authMethod":   authMethod,
		"count":        pg.Count,
		"samples":      samples,
	})
}

// handleAwxOpenBrowser: POST /api/awx/open-browser opens the AWX console in an isolated
// Edge/Chrome window with certificate bypass and private-network bridge support.
func (s *AgentServer) handleAwxOpenBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	baseURL, _, _, _, defScope, insecure, _ := s.getAwxSnapshot()

	var req struct {
		URL          string `json:"url"`
		DefaultScope string `json:"defaultScope"`
		InsecureTLS  *bool  `json:"insecureTls"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if strings.TrimSpace(req.DefaultScope) != "" {
		defScope = strings.TrimSpace(req.DefaultScope)
	}
	if req.InsecureTLS != nil {
		insecure = *req.InsecureTLS
	}
	if strings.TrimSpace(req.URL) != "" {
		b, sc, _ := parseAwxTargetInput(req.URL)
		if b != "" {
			baseURL = b
		}
		if req.DefaultScope == "" && sc != "" {
			defScope = sc
		}
	}
	consoleURL := buildAwxConsoleURL(baseURL, defScope, req.URL)
	if consoleURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok":    false,
			"error": "AWX 주소가 설정되지 않았습니다. 주소를 입력한 뒤 다시 시도하세요.",
		})
		return
	}
	var spkiHashes []string
	if insecure && strings.HasPrefix(strings.ToLower(consoleURL), "https://") {
		spkiHashes = probeServerSPKIHashes(consoleURL, 2500*time.Millisecond)
	}
	browser, bypassMethod, err := launcher.OpenAWX(consoleURL, spkiHashes, insecure)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok":    false,
			"url":   consoleURL,
			"error": fmt.Sprintf("AWX 창 실행 실패: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"url":          consoleURL,
		"browser":      browser,
		"bypassMethod": bypassMethod,
	})
}

// handleAwxBridgeState: GET/POST /api/awx/bridge/state manages live state synced from an open Edge/Chrome AWX tab.
func (s *AgentServer) handleAwxBridgeState(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		snap := s.getBridgeSnapshot()
		s.bridgeMu.RLock()
		hosts := make([]string, len(s.bridgeState.PageHosts))
		copy(hosts, s.bridgeState.PageHosts)
		s.bridgeMu.RUnlock()
		snap["pageHosts"] = hosts
		writeJSON(w, http.StatusOK, snap)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	var req AwxBridgeState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	cleanHosts := make([]string, 0, len(req.PageHosts))
	for _, h := range req.PageHosts {
		n := strings.ToLower(strings.TrimSpace(h))
		if n != "" {
			cleanHosts = append(cleanHosts, n)
		}
	}
	req.PageHosts = cleanHosts
	req.Connected = true
	req.UpdatedAt = time.Now()
	if req.URL != "" && (req.ScopeID == "" || req.Origin == "") {
		b, sc, pg := parseAwxTargetInput(req.URL)
		if req.Origin == "" && b != "" {
			req.Origin = b
		}
		if req.ScopeID == "" && sc != "" {
			st, sid, _ := normalizeAwxScope(sc, "")
			req.ScopeType = st
			req.ScopeID = sid
		}
		if req.Page == 0 && pg > 0 {
			req.Page = pg
		}
	}

	s.bridgeMu.Lock()
	s.bridgeState = req
	s.bridgeMu.Unlock()

	snap := s.getBridgeSnapshot()
	s.broadcastEvent("AWX_BRIDGE_UPDATED", "", snap)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "bridge": snap})
}

// handleAwxBridgeCollect: POST /api/awx/bridge/collect asks connected AWX browser tabs to collect hosts live.
func (s *AgentServer) handleAwxBridgeCollect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Mode    string `json:"mode"` // "page" or "all"
		Exclude string `json:"exclude"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Mode == "" {
		req.Mode = "all"
	}

	if s.wsHub == nil || s.wsHub.BridgeCount() == 0 {
		// If HTTP-only snapshot was pushed recently for page mode, still allow page collection cleanly
		s.bridgeMu.RLock()
		st := s.bridgeState
		s.bridgeMu.RUnlock()
		if req.Mode == "page" && st.Connected && time.Since(st.UpdatedAt) < 2*time.Minute && len(st.PageHosts) > 0 {
			filtered, excluded, results := filterHostList(st.PageHosts, req.Exclude)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":            true,
				"source":        "browser-bridge-cache",
				"label":         st.Label,
				"scopeType":     st.ScopeType,
				"scopeId":       st.ScopeID,
				"rawCount":      len(st.PageHosts),
				"count":         len(filtered),
				"hosts":         filtered,
				"excludedHosts": excluded,
				"results":       results,
			})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "연결된 AWX 브라우저 탭이 없습니다"})
		return
	}

	b := make([]byte, 4)
	_, _ = rand.Read(b)
	reqID := fmt.Sprintf("brg_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
	ch := make(chan AwxBridgeResult, 1)

	s.bridgeMu.Lock()
	s.bridgePending[reqID] = ch
	s.bridgeMu.Unlock()

	defer func() {
		s.bridgeMu.Lock()
		delete(s.bridgePending, reqID)
		s.bridgeMu.Unlock()
	}()

	s.wsHub.BroadcastJSON(map[string]interface{}{
		"event": "AWX_BRIDGE_COMMAND",
		"data": map[string]interface{}{
			"requestId": reqID,
			"mode":      req.Mode,
			"exclude":   req.Exclude,
		},
	})

	waitTimeout := 22 * time.Second
	if req.Mode == "page" {
		waitTimeout = 7 * time.Second
	}

	select {
	case res := <-ch:
		if !res.OK {
			msg := res.Error
			if msg == "" {
				msg = "열린 AWX 탭에서 수집에 실패했습니다"
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
			return
		}
		rawCount := res.RawCount
		if rawCount < len(res.Hosts) {
			rawCount = len(res.Hosts)
		}
		filtered, excluded, results := filterHostList(res.Hosts, req.Exclude)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":            true,
			"source":        "browser-bridge",
			"label":         res.Label,
			"scopeType":     res.ScopeType,
			"scopeId":       res.ScopeID,
			"page":          res.Page,
			"rawCount":      rawCount,
			"count":         len(filtered),
			"hosts":         filtered,
			"excludedHosts": excluded,
			"results":       results,
		})
	case <-time.After(waitTimeout):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "AWX 브라우저 탭 응답 시간 초과"})
	}
}

// handleAwxBridgeResult: POST /api/awx/bridge/result receives collected host data from a bridged AWX browser tab.
func (s *AgentServer) handleAwxBridgeResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var res AwxBridgeResult
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	s.bridgeMu.RLock()
	ch, ok := s.bridgePending[res.RequestID]
	s.bridgeMu.RUnlock()

	if ok {
		select {
		case ch <- res:
		default:
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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
			if isLocalHostIP(u.IP, myIP) {
				name = u.Name
				registered = true
				if s.detectedIP != u.IP && u.IP != "127.0.0.1" && u.IP != "localhost" {
					s.detectedIP = u.IP
					myIP = u.IP
				}
				break
			}
		}
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ip":          myIP,
		"name":        name,
		"registered":  registered,
		"sharedDir":   s.config.SharedDir,
		"usersSource": s.usersSource,
		"usersError":  errMsg,
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"updatedAt":  time.Now().Format(time.RFC3339),
			"users":      []User{},
			"sourcePath": s.usersSource,
			"sharedDir":  s.config.SharedDir,
			"error":      err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"updatedAt":  uf.UpdatedAt,
		"users":      uf.Users,
		"sourcePath": s.usersSource,
		"sharedDir":  s.config.SharedDir,
	})
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

	peerDelivered := true
	var peerErrStr string

	// Direct HTTP notification to checker (자기 자신 호출 방지)
	if sess.Checker.IP != "" && sess.Checker.IP != s.detectedIP && sess.Checker.IP != "127.0.0.1" && sess.Checker.IP != sess.Worker.IP {
		url := fmt.Sprintf("http://%s:%d/api/peer/notify", sess.Checker.IP, s.config.ListenPort)
		err := s.postPeerJSON(url, map[string]interface{}{
			"type":      "NEW_SESSION",
			"sessionId": sess.SessionId,
			"session":   sess,
		})
		if err != nil {
			peerDelivered = false
			peerErrStr = err.Error()
			log.Printf("[PEER] Warning: Checker at %s could not be reached directly: %v\n", url, err)
		} else {
			peerDelivered = true
		}
	}

	// Broadcast event
	s.broadcastEvent("NEW_SESSION", sess.SessionId, sess)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessionId":     sess.SessionId,
		"status":        sess.Status,
		"peerDelivered": peerDelivered,
		"peerError":     peerErrStr,
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

	// Notify worker async so the requester's screen updates immediately!
	if sess.Worker.IP != "" && sess.Worker.IP != s.detectedIP && sess.Worker.IP != "127.0.0.1" {
		go func(workerIP string, sessCopy Session) {
			url := fmt.Sprintf("http://%s:%d/api/peer/update", workerIP, s.config.ListenPort)
			s.postPeerJSON(url, map[string]interface{}{
				"type":      "SESSION_REASSIGNED",
				"sessionId": sessCopy.SessionId,
				"session":   sessCopy,
				"checker":   sessCopy.Checker,
				"status":    sessCopy.Status,
			})
		}(sess.Worker.IP, *sess)
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
		Session   *Session    `json:"session"`
		Data      interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		// Save received session to local disk so /api/session/list and /api/session/status find it
		if payload.Session != nil {
			if saveErr := s.saveSession(payload.Session); saveErr != nil {
				log.Printf("[PEER] Error saving received session %s: %v\n", payload.SessionId, saveErr)
			} else {
				log.Printf("[PEER] Successfully saved received session %s from worker %s (%s)\n", payload.SessionId, payload.Session.Worker.Name, payload.Session.Worker.IP)
			}
		}
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
		Type        string       `json:"type"`
		SessionId   string       `json:"sessionId"`
		Status      string       `json:"status"`
		Data        interface{}  `json:"data"`
		CheckerData *CheckerData `json:"checkerData"`
		Checker     *User        `json:"checker"`
		Session     *Session     `json:"session"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		// Update local session on disk if it exists
		if sess, err := s.loadSession(payload.SessionId); err == nil {
			if payload.Status != "" {
				sess.Status = payload.Status
			}
			if payload.CheckerData != nil {
				sess.CheckerData = payload.CheckerData
			}
			if payload.Checker != nil {
				sess.Checker = *payload.Checker
			}
			if payload.Session != nil {
				sess.Checker = payload.Session.Checker
				sess.CheckerHistory = payload.Session.CheckerHistory
			}
			if payload.Status == "APPROVED" || payload.Status == "REJECTED" {
				sess.DecidedAt = time.Now().Format(time.RFC3339)
			}
			_ = s.saveSession(sess)
			log.Printf("[PEER] Updated local session %s on disk (type=%s, status=%s, checker=%s)\n", payload.SessionId, payload.Type, payload.Status, sess.Checker.Name)
		}
		eventType := "PEER_UPDATE"
		if payload.Type != "" {
			eventType = payload.Type
		}
		s.broadcastEvent(eventType, payload.SessionId, payload)
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
	isBridge := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("role")), "bridge")
	if !isBridge {
		s.recordClientPing()
	}
	client := &WSClient{conn: conn, IsBridge: isBridge}
	s.wsHub.Add(client)
	defer func() {
		s.wsHub.Remove(client)
		conn.Close()
		if isBridge && s.wsHub.BridgeCount() == 0 {
			s.bridgeMu.Lock()
			s.bridgeState.Connected = false
			st := s.bridgeState
			s.bridgeMu.Unlock()
			s.broadcastEvent("AWX_BRIDGE_UPDATED", "", map[string]interface{}{
				"connected":   false,
				"bridgeCount": 0,
				"state":       st,
			})
		}
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
			if !isBridge {
				s.recordClientPing()
			}
			_ = client.WritePong(payload)
			continue
		}
		if opcode == 0x01 { // Text frame
			if !isBridge {
				s.recordClientPing()
			}
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

// findAppWindow locates the visible Chromium/Edge application window handle for the checker UI.
func findAppWindow() uintptr {
	if runtime.GOOS != "windows" {
		return 0
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	enumWindows := user32.NewProc("EnumWindows")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")
	isWindowVisible := user32.NewProc("IsWindowVisible")
	getClassName := user32.NewProc("GetClassNameW")

	const (
		WM_GETTEXT       = 0x000D
		SMTO_ABORTIFHUNG = 0x0002
		SMTO_NORMAL      = 0x0000
	)

	var hwnd uintptr
	cb := syscall.NewCallback(func(h uintptr, lParam uintptr) uintptr {
		vis, _, _ := isWindowVisible.Call(h)
		if vis == 0 {
			return 1 // continue
		}
		clsBuf := make([]uint16, 128)
		clsLen, _, _ := getClassName.Call(h, uintptr(unsafe.Pointer(&clsBuf[0])), uintptr(len(clsBuf)))
		if clsLen == 0 || !strings.HasPrefix(syscall.UTF16ToString(clsBuf[:clsLen]), "Chrome_WidgetWin_") {
			return 1 // continue
		}
		buf := make([]uint16, 256)
		var result uintptr
		ret, _, _ := sendMessageTimeout.Call(
			h,
			uintptr(WM_GETTEXT),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(SMTO_ABORTIFHUNG|SMTO_NORMAL),
			uintptr(50),
			uintptr(unsafe.Pointer(&result)),
		)
		if ret != 0 && result > 0 {
			t := syscall.UTF16ToString(buf)
			if strings.Contains(t, "작업대상 호스트 교차 검증기") {
				hwnd = h
				return 0 // stop
			}
		}
		return 1 // continue
	})
	enumWindows.Call(cb, 0)
	return hwnd
}

// isWindowTopmost reads WS_EX_TOPMOST on hwnd.
func isWindowTopmost(hwnd uintptr) bool {
	if runtime.GOOS != "windows" || hwnd == 0 {
		return false
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	getWindowLong := user32.NewProc("GetWindowLongW")
	const gwlExStyle = ^uintptr(19) // -20 (GWL_EXSTYLE)
	const wsExTopmost = uintptr(0x00000008)
	exStyle, _, _ := getWindowLong.Call(hwnd, gwlExStyle)
	return (exStyle & wsExTopmost) != 0
}

// getWindowTopmost returns whether the app window was found and whether WS_EX_TOPMOST is active.
func getWindowTopmost() (found bool, pinned bool) {
	hwnd := findAppWindow()
	if hwnd == 0 {
		return false, false
	}
	return true, isWindowTopmost(hwnd)
}

// setWindowTopmost toggles HWND_TOPMOST / HWND_NOTOPMOST without activating/stealing focus.
func setWindowTopmost(pinned bool) (found bool, actualPinned bool) {
	hwnd := findAppWindow()
	if hwnd == 0 {
		return false, false
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	setWindowPos := user32.NewProc("SetWindowPos")
	const (
		hwndTopmost   = ^uintptr(0) // -1 (HWND_TOPMOST)
		hwndNotopmost = ^uintptr(1) // -2 (HWND_NOTOPMOST)
		swpNosize     = uintptr(0x0001)
		swpNomove     = uintptr(0x0002)
		swpNoactivate = uintptr(0x0010)
	)
	targetZ := hwndNotopmost
	if pinned {
		targetZ = hwndTopmost
	}
	setWindowPos.Call(hwnd, targetZ, 0, 0, 0, 0, swpNomove|swpNosize|swpNoactivate)
	return true, isWindowTopmost(hwnd)
}

// flashAppWindow triggers a taskbar flash on Windows for the app window.
func flashAppWindow() {
	hwnd := findAppWindow()
	if hwnd == 0 {
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	flashWindowEx := user32.NewProc("FlashWindowEx")
	type FLASHWINFO struct {
		CbSize    uint32
		Hwnd      uintptr
		DwFlags   uint32
		UCount    uint32
		DwTimeout uint32
	}
	const FLASHW_ALL = 0x00000003
	const FLASHW_TIMERNOFG = 0x0000000C
	info := FLASHWINFO{
		CbSize:    uint32(unsafe.Sizeof(FLASHWINFO{})),
		Hwnd:      hwnd,
		DwFlags:   FLASHW_ALL | FLASHW_TIMERNOFG,
		UCount:    0,
		DwTimeout: 0,
	}
	flashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
}

// restoreAppWindow brings the app window to the front and restores it if minimized.
func restoreAppWindow() {
	hwnd := findAppWindow()
	if hwnd == 0 {
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")
	setForegroundWindow := user32.NewProc("SetForegroundWindow")
	const SW_RESTORE = 9
	showWindow.Call(hwnd, SW_RESTORE)
	setForegroundWindow.Call(hwnd)
}

// handleWindowFlash handles POST /api/window/flash
func (s *AgentServer) handleWindowFlash(w http.ResponseWriter, r *http.Request) {
	flashAppWindow()
	writeJSON(w, http.StatusOK, map[string]interface{}{"flashed": true})
}

// handleWindowRestore handles POST /api/window/restore
func (s *AgentServer) handleWindowRestore(w http.ResponseWriter, r *http.Request) {
	restoreAppWindow()
	writeJSON(w, http.StatusOK, map[string]interface{}{"restored": true})
}

// handleWindowPin handles GET/POST /api/window/pin to query or toggle OS topmost state.
func (s *AgentServer) handleWindowPin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		found, pinned := getWindowTopmost()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"found":  found,
			"pinned": pinned,
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed"})
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}
	found, actual := setWindowTopmost(req.Pinned)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"found":     found,
		"pinned":    actual,
		"requested": req.Pinned,
	})
}

func waitForServerReady(healthURL string, timeout time.Duration) bool {
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
				log.Printf("[HEALTH] 로컬 HTTP 서버 준비 완료 (%s)\n", healthURL)
				return true
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	log.Printf("[경고] 로컬 HTTP 서버 준비 대기 시간 초과 (%v), 브라우저 실행을 계속 진행합니다.\n", timeout)
	return false
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
			if !isFlagPassed("bind-ip") && fileCfg.BindIP != "" {
				cfg.BindIP = fileCfg.BindIP
			}
			if fileCfg.AwxBaseURL != "" {
				cfg.AwxBaseURL = fileCfg.AwxBaseURL
			}
			if fileCfg.AwxUsername != "" {
				cfg.AwxUsername = fileCfg.AwxUsername
			}
			if fileCfg.AwxPassword != "" {
				cfg.AwxPassword = fileCfg.AwxPassword
			}
			if fileCfg.AwxToken != "" {
				cfg.AwxToken = fileCfg.AwxToken
			}
			if fileCfg.AwxDefaultScope != "" {
				cfg.AwxDefaultScope = fileCfg.AwxDefaultScope
			}
			if fileCfg.AwxInsecureTLS != nil {
				cfg.AwxInsecureTLS = fileCfg.AwxInsecureTLS
			}
		} else {
			log.Printf("[CONFIG 경고] %s 파싱 실패 (JSON 문법 오류): %v\n", configPath, err)
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
	log.Printf("  >> 사용자 명단 경로:   %s\n", server.usersSource)
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

	// Warm up configured default AWX inventory cache in background
	go server.WarmupAwxCache("")

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
	waitForServerReady(fmt.Sprintf("http://127.0.0.1:%d/api/health", cfg.ListenPort), 10*time.Second)

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
