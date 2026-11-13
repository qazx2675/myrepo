package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"host-cross-checker/internal/launcher"
)

// setupTestServer creates an isolated AgentServer with a temp directory.
func setupTestServer(t *testing.T) (*AgentServer, *httptest.Server) {
	t.Helper()
	tempDir := t.TempDir()

	cfg := Config{
		SharedDir:                tempDir,
		ListenPort:               18080,
		PollIntervalMs:           50, // fast polling for tests
		AutoOpenBrowserOnRequest: false,
		BindIP:                   "192.168.0.23",
	}

	server, err := NewAgentServer(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize test agent server: %v", err)
	}

	ts := httptest.NewServer(server.Routes())
	t.Cleanup(func() {
		ts.Close()
		server.Stop()
	})

	return server, ts
}

// TestCORSHeadersAndOptions verifies CORS headers and OPTIONS response.
func TestCORSHeadersAndOptions(t *testing.T) {
	_, ts := setupTestServer(t)

	// Preflight OPTIONS
	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/session/create", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK on OPTIONS, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *, got %s", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Errorf("Expected Access-Control-Allow-Private-Network: true, got %s", resp.Header.Get("Access-Control-Allow-Private-Network"))
	}
}

// TestUserRegistrationAndPersistence verifies user registration and users.json persistence.
func TestUserRegistrationAndPersistence(t *testing.T) {
	server, ts := setupTestServer(t)

	// 1. Initial users list
	resp, err := http.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var uf UsersFile
	if err := json.NewDecoder(resp.Body).Decode(&uf); err != nil {
		t.Fatal(err)
	}
	if len(uf.Users) == 0 {
		t.Fatal("Expected default sample users, got empty list")
	}

	// 2. /api/me check
	respMe, err := http.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	defer respMe.Body.Close()

	var meResp map[string]interface{}
	if err := json.NewDecoder(respMe.Body).Decode(&meResp); err != nil {
		t.Fatal(err)
	}
	if meResp["ip"] != "192.168.0.23" {
		t.Errorf("Expected IP 192.168.0.23, got %v", meResp["ip"])
	}
	if meResp["name"] != "김철수" {
		t.Errorf("Expected name 김철수, got %v", meResp["name"])
	}
	if meResp["registered"] != true {
		t.Errorf("Expected registered=true, got %v", meResp["registered"])
	}

	// 3. Register a new user
	newUser := User{
		Name: "홍길동",
		IP:   "10.10.10.10",
		Role: "테스터",
	}
	userBytes, _ := json.Marshal(newUser)
	regResp, err := http.Post(ts.URL+"/api/users/register", "application/json", bytes.NewReader(userBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer regResp.Body.Close()

	if regResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on register, got %d", regResp.StatusCode)
	}

	// 4. Verify file persistence on disk
	usersOnDisk, err := server.readUsers()
	if err != nil {
		t.Fatalf("Failed to read users.json: %v", err)
	}
	found := false
	for _, u := range usersOnDisk.Users {
		if u.IP == "10.10.10.10" && u.Name == "홍길동" && u.Role == "테스터" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Newly registered user not persisted to users.json!")
	}
}

// TestSessionCreateAndPersistence verifies session creation and disk saving.
func TestSessionCreateAndPersistence(t *testing.T) {
	server, ts := setupTestServer(t)

	sessReq := Session{
		Worker: User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata: Metadata{
			IncidentId:      "INC-2026-9999",
			SpecialNotes:    "긴급 배포 작업",
			AwxContextLabel: "Prod_Inventory_Web",
			ExcludePattern:  "bastion*|test*",
		},
		ActiveFields: ActiveFields{
			Awx:      true,
			Detail:   true,
			Incident: true,
			Target:   false,
		},
		WorkerData: WorkerData{
			AwxHosts:      []string{"web01", "web02", "web03", "web04"},
			ExcludedHosts: []string{"bastion01"},
			RawAwxCount:   5,
			DetailHosts:   []string{"web01", "web02", "web03", "web04"},
			IncidentHosts: []string{"web01", "web02", "web03", "web04"},
		},
	}
	bodyBytes, _ := json.Marshal(sessReq)

	start := time.Now()
	resp, err := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if time.Since(start) > 3*time.Second {
		t.Fatalf("Session create exceeded latency limit of 3000ms: %v", time.Since(start))
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on create session, got %d", resp.StatusCode)
	}

	var resData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
		t.Fatal(err)
	}
	sessionId, ok := resData["sessionId"].(string)
	if !ok || !strings.HasPrefix(sessionId, "req_") {
		t.Fatalf("Invalid sessionId format: %v", resData["sessionId"])
	}
	if resData["status"] != "PENDING" {
		t.Fatalf("Expected status PENDING, got %v", resData["status"])
	}

	// Verify session file on disk
	sessFile := filepath.Join(server.config.SharedDir, "sessions", sessionId+".json")
	if _, err := os.Stat(sessFile); os.IsNotExist(err) {
		t.Fatalf("Session file %s not found on disk", sessFile)
	}
}

// TestUnauthorizedAccess403 verifies 403 Forbidden for unauthorized IPs.
func TestUnauthorizedAccess403(t *testing.T) {
	_, ts := setupTestServer(t)

	// Create a session
	sessReq := Session{
		Worker: User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "확인자B", IP: "192.168.1.20"},
		WorkerData: WorkerData{
			DetailHosts: []string{"web01", "web02"},
		},
	}
	bodyBytes, _ := json.Marshal(sessReq)
	resp, err := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	var resData map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&resData)
	resp.Body.Close()
	sessionId := resData["sessionId"].(string)

	// 1. Status query with unauthorized IP
	unauthUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=10.99.99.99", ts.URL, sessionId)
	respUnauth, err := http.Get(unauthUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer respUnauth.Body.Close()

	if respUnauth.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for unauthorized clientIp, got %d", respUnauth.StatusCode)
	}

	// 2. Decision post with unauthorized IP
	decBody := map[string]interface{}{
		"sessionId": sessionId,
		"checkerIp": "10.99.99.99",
		"checkerData": map[string]interface{}{
			"decision":   "APPROVED",
			"isAllMatch": true,
		},
	}
	decBytes, _ := json.Marshal(decBody)
	respDec, err := http.Post(ts.URL+"/api/session/decision", "application/json", bytes.NewReader(decBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer respDec.Body.Close()

	if respDec.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden on decision with unauthorized IP, got %d", respDec.StatusCode)
	}
}

// TestBlindPreviewBeforeDecision verifies blinding logic for checkers in PENDING/CHECKING status.
func TestBlindPreviewBeforeDecision(t *testing.T) {
	_, ts := setupTestServer(t)

	// Build 30 hosts
	hosts30 := make([]string, 30)
	for i := 0; i < 30; i++ {
		hosts30[i] = fmt.Sprintf("server-%02d.prod.corp", i+1)
	}

	sessReq := Session{
		Worker: User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "확인자B", IP: "192.168.1.20"},
		ActiveFields: ActiveFields{Detail: true, Incident: true, Awx: true},
		WorkerData: WorkerData{
			DetailHosts:   hosts30,
			IncidentHosts: hosts30,
			AwxHosts:      hosts30,
			RawAwxCount:   30,
		},
	}
	bodyBytes, _ := json.Marshal(sessReq)
	resp, err := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	var resData map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&resData)
	resp.Body.Close()
	sessionId := resData["sessionId"].(string)

	// 1. Checker queries status (PENDING status -> BLINDED)
	checkerStatusUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=192.168.1.20", ts.URL, sessionId)
	respChecker, err := http.Get(checkerStatusUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer respChecker.Body.Close()

	if respChecker.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for checker, got %d", respChecker.StatusCode)
	}

	var checkerResp map[string]interface{}
	if err := json.NewDecoder(respChecker.Body).Decode(&checkerResp); err != nil {
		t.Fatal(err)
	}

	if checkerResp["blinded"] != true {
		t.Errorf("Expected blinded=true in checker response")
	}

	wd, ok := checkerResp["workerData"].(map[string]interface{})
	if !ok {
		t.Fatalf("workerData missing or not an object")
	}

	// Verify workerData hosts arrays are empty (preventing scraping)
	detailHosts, _ := wd["detailHosts"].([]interface{})
	if len(detailHosts) != 0 {
		t.Fatalf("Expected workerData.detailHosts to be empty [], got %d hosts", len(detailHosts))
	}
	incidentHosts, _ := wd["incidentHosts"].([]interface{})
	if len(incidentHosts) != 0 {
		t.Fatalf("Expected workerData.incidentHosts to be empty [], got %d hosts", len(incidentHosts))
	}
	awxHosts, _ := wd["awxHosts"].([]interface{})
	if len(awxHosts) != 0 {
		t.Fatalf("Expected workerData.awxHosts to be empty [], got %d hosts", len(awxHosts))
	}

	// Verify preview: only top 3 hosts visible
	detailPrev, ok := wd["detailHostsPreview"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected detailHostsPreview in blinded workerData")
	}
	previewHosts, _ := detailPrev["previewHosts"].([]interface{})
	if len(previewHosts) != 3 {
		t.Fatalf("Expected 3 previewHosts, got %d", len(previewHosts))
	}
	if detailPrev["totalCount"].(float64) != 30 {
		t.Errorf("Expected totalCount 30, got %v", detailPrev["totalCount"])
	}
	if detailPrev["blindedCount"].(float64) != 27 {
		t.Errorf("Expected blindedCount 27, got %v", detailPrev["blindedCount"])
	}
	if wd["awxCount"].(float64) != 30 {
		t.Errorf("Expected awxCount 30, got %v", wd["awxCount"])
	}

	// 2. Worker queries status -> UNBLINDED
	workerStatusUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=192.168.1.10", ts.URL, sessionId)
	respWorker, err := http.Get(workerStatusUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer respWorker.Body.Close()

	var workerResp Session
	if err := json.NewDecoder(respWorker.Body).Decode(&workerResp); err != nil {
		t.Fatal(err)
	}
	if len(workerResp.WorkerData.DetailHosts) != 30 {
		t.Fatalf("Expected worker to see full 30 detailHosts, got %d", len(workerResp.WorkerData.DetailHosts))
	}
}

// TestReassignZeroCooldownAndHistory verifies reassignment with zero cooldown and history.
func TestReassignZeroCooldownAndHistory(t *testing.T) {
	_, ts := setupTestServer(t)

	sessReq := Session{
		Worker:  User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "원확인자", IP: "192.168.1.20"},
		WorkerData: WorkerData{
			DetailHosts: []string{"web01"},
		},
	}
	b, _ := json.Marshal(sessReq)
	resp, _ := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(b))
	var resData map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&resData)
	resp.Body.Close()
	sessionId := resData["sessionId"].(string)

	// Reassign immediately (zero cooldown)
	reassignReq := map[string]interface{}{
		"sessionId": sessionId,
		"newChecker": User{
			Name: "신규확인자",
			IP:   "192.168.1.30",
			Role: "확인자",
		},
	}
	rb, _ := json.Marshal(reassignReq)
	reassignResp, err := http.Post(ts.URL+"/api/session/reassign", "application/json", bytes.NewReader(rb))
	if err != nil {
		t.Fatal(err)
	}
	defer reassignResp.Body.Close()

	if reassignResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on reassign, got %d", reassignResp.StatusCode)
	}

	// Verify session history
	statusUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=192.168.1.10", ts.URL, sessionId)
	statusResp, err := http.Get(statusUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()

	var updatedSession Session
	if err := json.NewDecoder(statusResp.Body).Decode(&updatedSession); err != nil {
		t.Fatal(err)
	}

	if updatedSession.Checker.Name != "신규확인자" || updatedSession.Checker.IP != "192.168.1.30" {
		t.Fatalf("Checker was not updated to newChecker: %+v", updatedSession.Checker)
	}
	if len(updatedSession.CheckerHistory) != 1 {
		t.Fatalf("Expected 1 checkerHistory item, got %d", len(updatedSession.CheckerHistory))
	}
	hist := updatedSession.CheckerHistory[0]
	if hist.Checker.Name != "원확인자" || hist.Checker.IP != "192.168.1.20" {
		t.Errorf("Unexpected history checker: %+v", hist.Checker)
	}
	if hist.ReassignedAt == "" {
		t.Errorf("Expected ReassignedAt timestamp in history")
	}
}

// TestDecisionValidation verifies decision validation rules (APPROVED vs REJECTED).
func TestDecisionValidation(t *testing.T) {
	_, ts := setupTestServer(t)

	sessReq := Session{
		Worker:  User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "확인자B", IP: "192.168.1.20"},
		WorkerData: WorkerData{
			DetailHosts: []string{"web01", "web02"},
		},
	}
	b, _ := json.Marshal(sessReq)
	resp, _ := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(b))
	var resData map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&resData)
	resp.Body.Close()
	sessionId := resData["sessionId"].(string)

	// Case 1: isAllMatch == false and decision == APPROVED -> MUST FAIL (400 Bad Request)
	invalidApproveReq := map[string]interface{}{
		"sessionId": sessionId,
		"checkerIp": "192.168.1.20",
		"checkerData": map[string]interface{}{
			"detailHosts":     []string{"web01"},
			"incidentHosts":   []string{"web01"},
			"isAllMatch":      false, // Mismatch!
			"decision":        "APPROVED",
			"decisionComment": "강제 승인 시도",
		},
	}
	invBytes, _ := json.Marshal(invalidApproveReq)
	respInv, err := http.Post(ts.URL+"/api/session/decision", "application/json", bytes.NewReader(invBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer respInv.Body.Close()

	if respInv.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request when approving with isAllMatch=false, got %d", respInv.StatusCode)
	}

	// Case 2: REJECTED with reason -> MUST SUCCEED (200 OK)
	rejectReq := map[string]interface{}{
		"sessionId": sessionId,
		"checkerIp": "192.168.1.20",
		"checkerData": map[string]interface{}{
			"detailHosts":     []string{"web01"},
			"incidentHosts":   []string{"web01"},
			"isAllMatch":      false,
			"decision":        "REJECTED",
			"decisionComment": "호스트 1대 누락으로 반려합니다.",
		},
	}
	rejBytes, _ := json.Marshal(rejectReq)
	respRej, err := http.Post(ts.URL+"/api/session/decision", "application/json", bytes.NewReader(rejBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer respRej.Body.Close()

	if respRej.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on valid rejection, got %d", respRej.StatusCode)
	}

	// Case 3: Create another session and test valid APPROVAL (isAllMatch == true)
	resp2, _ := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(b))
	var resData2 map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&resData2)
	resp2.Body.Close()
	sessionId2 := resData2["sessionId"].(string)

	approveReq := map[string]interface{}{
		"sessionId": sessionId2,
		"checkerIp": "192.168.1.20",
		"checkerData": map[string]interface{}{
			"detailHosts":     []string{"web01", "web02"},
			"incidentHosts":   []string{"web01", "web02"},
			"isAllMatch":      true,
			"decision":        "APPROVED",
			"decisionComment": "전수 일치 확인 완료",
		},
	}
	appBytes, _ := json.Marshal(approveReq)
	respApp, err := http.Post(ts.URL+"/api/session/decision", "application/json", bytes.NewReader(appBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer respApp.Body.Close()

	if respApp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on valid approval, got %d", respApp.StatusCode)
	}

	// Verify unblinded view after decision
	statusUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=192.168.1.20", ts.URL, sessionId2)
	statusResp, err := http.Get(statusUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()

	var fullSession Session
	if err := json.NewDecoder(statusResp.Body).Decode(&fullSession); err != nil {
		t.Fatal(err)
	}
	if fullSession.Status != "APPROVED" {
		t.Fatalf("Expected status APPROVED, got %s", fullSession.Status)
	}
	if len(fullSession.WorkerData.DetailHosts) != 2 {
		t.Fatalf("Expected checker to see full unblinded hosts after decision, got %d", len(fullSession.WorkerData.DetailHosts))
	}
}

// TestLatencyUnder3000ms tests that API latency is strictly under 3000ms.
func TestLatencyUnder3000ms(t *testing.T) {
	_, ts := setupTestServer(t)

	start := time.Now()

	// Create
	sessReq := Session{
		Worker:  User{Name: "작업자A", IP: "192.168.1.10"},
		Checker: User{Name: "확인자B", IP: "192.168.1.20"},
		WorkerData: WorkerData{
			DetailHosts: []string{"web01", "web02"},
		},
	}
	b, _ := json.Marshal(sessReq)
	resp, err := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var resData map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&resData)
	resp.Body.Close()
	sessionId := resData["sessionId"].(string)

	// Status
	statusUrl := fmt.Sprintf("%s/api/session/status?sessionId=%s&clientIp=192.168.1.10", ts.URL, sessionId)
	sResp, err := http.Get(statusUrl)
	if err != nil {
		t.Fatal(err)
	}
	sResp.Body.Close()

	// Decision
	decReq := map[string]interface{}{
		"sessionId": sessionId,
		"checkerIp": "192.168.1.20",
		"checkerData": map[string]interface{}{
			"isAllMatch": true,
			"decision":   "APPROVED",
		},
	}
	db, _ := json.Marshal(decReq)
	dResp, err := http.Post(ts.URL+"/api/session/decision", "application/json", bytes.NewReader(db))
	if err != nil {
		t.Fatal(err)
	}
	dResp.Body.Close()

	totalDuration := time.Since(start)
	if totalDuration >= 3000*time.Millisecond {
		t.Fatalf("Total round-trip latency exceeded 3000ms: %v", totalDuration)
	}
	t.Logf("Total latency for full workflow: %v (< 3000ms limit)", totalDuration)
}

// TestPeerNotificationsAndSSE verifies SSE connection and peer notify/update broadcasting.
func TestPeerNotificationsAndSSE(t *testing.T) {
	_, ts := setupTestServer(t)

	// Connect SSE client
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for SSE, got %d", resp.StatusCode)
	}

	receivedEvents := make(chan string, 10)
	reader := bufio.NewReader(resp.Body)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				receivedEvents <- dataStr
			}
		}
	}()

	// 1. Send peer notify
	notifyPayload := map[string]interface{}{
		"type":      "NEW_SESSION",
		"sessionId": "req_test_001",
		"data":      "test_notification",
	}
	nb, _ := json.Marshal(notifyPayload)
	nResp, err := http.Post(ts.URL+"/api/peer/notify", "application/json", bytes.NewReader(nb))
	if err != nil {
		t.Fatal(err)
	}
	nResp.Body.Close()

	// Wait for SSE broadcast
	select {
	case evt := <-receivedEvents:
		if !strings.Contains(evt, "NEW_SESSION") || !strings.Contains(evt, "req_test_001") {
			t.Fatalf("Unexpected SSE event data: %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for SSE notify event")
	}

	// 2. Send peer update
	updatePayload := map[string]interface{}{
		"type":      "SESSION_DECIDED",
		"sessionId": "req_test_001",
		"status":    "APPROVED",
	}
	ub, _ := json.Marshal(updatePayload)
	uResp, err := http.Post(ts.URL+"/api/peer/update", "application/json", bytes.NewReader(ub))
	if err != nil {
		t.Fatal(err)
	}
	uResp.Body.Close()

	// Wait for SSE broadcast
	select {
	case evt := <-receivedEvents:
		if !strings.Contains(evt, "SESSION_DECIDED") {
			t.Fatalf("Unexpected SSE event data: %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for SSE update event")
	}
}

// TestBackgroundPolling verifies that modifying session files triggers SSE events.
func TestBackgroundPolling(t *testing.T) {
	server, ts := setupTestServer(t)
	server.StartBackgroundPolling()

	// Connect SSE client
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	receivedEvents := make(chan string, 10)
	reader := bufio.NewReader(resp.Body)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				receivedEvents <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Create initial session file directly on disk
	sessionId := "req_poll_test"
	sess := Session{
		SessionId: sessionId,
		Status:    "PENDING",
		Worker:    User{Name: "폴러", IP: "127.0.0.1"},
	}
	_ = server.saveSession(&sess)

	// Wait a bit for initial detection
	time.Sleep(150 * time.Millisecond)

	// Modify session file on disk
	sess.Status = "APPROVED"
	_ = server.saveSession(&sess)

	// Verify SSE broadcast triggered by background polling
	select {
	case evt := <-receivedEvents:
		if !strings.Contains(evt, "SESSION_UPDATED") && !strings.Contains(evt, "req_poll_test") {
			t.Fatalf("Unexpected polling SSE event: %s", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for polling SSE event")
	}
}

// TestCheckerUIEndpoint verifies that /checker returns 200 OK and HTML content.
func TestCheckerUIEndpoint(t *testing.T) {
	_, ts := setupTestServer(t)

	resp, err := http.Get(ts.URL + "/checker?sessionId=req_1234")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on /checker, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	if !strings.Contains(html, "AWX 작업대상 호스트 교차 검증기") {
		t.Fatalf("Checker HTML does not contain title header")
	}
	if !strings.Contains(html, "btnApprove") || !strings.Contains(html, "btnReject") {
		t.Fatalf("Checker HTML missing buttons")
	}
}

func makeWSClientFrame(opcode byte, payload []byte) []byte {
	length := len(payload)
	var header []byte
	mask := []byte{0x12, 0x34, 0x56, 0x78}
	if length < 126 {
		header = []byte{0x80 | opcode, 0x80 | byte(length)}
	} else if length < 65536 {
		header = make([]byte, 4)
		header[0] = 0x80 | opcode
		header[1] = 0x80 | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(length))
	}
	header = append(header, mask...)
	maskedPayload := make([]byte, length)
	for i := 0; i < length; i++ {
		maskedPayload[i] = payload[i] ^ mask[i%4]
	}
	return append(header, maskedPayload...)
}

// TestWebSocketRPCAndBroadcast verifies WebSocket connection, RPC requests and real-time broadcasting.
func TestWebSocketRPCAndBroadcast(t *testing.T) {
	_, ts := setupTestServer(t)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Perform WebSocket handshake
	wsKey := "dGhlIHNhbXBsZSBub25jZQ=="
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + wsKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	bufr := bufio.NewReader(conn)
	statusLine, err := bufr.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("Expected 101 Switching Protocols, got: %s", statusLine)
	}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// 1. Send RPC message over WebSocket: GET /api/me
	rpcReq := map[string]interface{}{
		"id":     "req-test-1",
		"method": "GET",
		"path":   "/api/me",
	}
	rpcBytes, _ := json.Marshal(rpcReq)
	frame := makeWSClientFrame(0x01, rpcBytes)
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}

	// Read response frame
	opcode, payload, err := wsReadMessage(bufr)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0x01 {
		t.Fatalf("Expected text opcode 1, got %d", opcode)
	}

	var rpcResp WSResponseMsg
	if err := json.Unmarshal(payload, &rpcResp); err != nil {
		t.Fatalf("Failed to decode WS response: %v, body: %s", err, payload)
	}
	if rpcResp.ID != "req-test-1" || rpcResp.Status != 200 {
		t.Fatalf("Unexpected WS RPC response: %+v", rpcResp)
	}

	// 2. Test event broadcast: Create session via HTTP POST and verify WebSocket receives NEW_SESSION event
	sessPayload := map[string]interface{}{
		"worker":   map[string]string{"name": "작업자1", "ip": "192.168.0.23"},
		"checker":  map[string]string{"name": "확인자1", "ip": "192.168.0.102"},
		"metadata": map[string]string{"incidentId": "INC-WS-001"},
		"activeFields": map[string]bool{
			"awx":      true,
			"detail":   true,
			"incident": true,
		},
		"workerData": map[string]interface{}{
			"awxHosts":      []string{"h1"},
			"detailHosts":   []string{"h1"},
			"incidentHosts": []string{"h1"},
		},
	}
	sessBody, _ := json.Marshal(sessPayload)
	httpResp, err := http.Post(ts.URL+"/api/session/create", "application/json", bytes.NewReader(sessBody))
	if err != nil || httpResp.StatusCode != 200 {
		t.Fatalf("Failed to create session via HTTP: %v", err)
	}

	// Read broadcast event on WebSocket
	opcodeEvt, payloadEvt, err := wsReadMessage(bufr)
	if err != nil {
		t.Fatal(err)
	}
	if opcodeEvt != 0x01 {
		t.Fatalf("Expected text opcode 1, got %d", opcodeEvt)
	}
	var evtMap map[string]interface{}
	if err := json.Unmarshal(payloadEvt, &evtMap); err != nil {
		t.Fatalf("Failed to decode event: %v", err)
	}
	if evtMap["event"] != "NEW_SESSION" {
		t.Fatalf("Expected event NEW_SESSION, got: %v", evtMap["event"])
	}
}

// TestStandaloneEndpoints verifies root HTML serving, JS bundle serving, health, ping, and bye.
func TestStandaloneEndpoints(t *testing.T) {
	_, ts := setupTestServer(t)

	// 1. GET / serves index.html
	respRoot, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer respRoot.Body.Close()
	if respRoot.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /, got %d", respRoot.StatusCode)
	}
	bodyRoot, _ := io.ReadAll(respRoot.Body)
	if !strings.Contains(string(bodyRoot), "awx-vld-standalone-root") {
		t.Errorf("Expected standalone root markup, got: %s", string(bodyRoot[:min(200, len(bodyRoot))]))
	}

	// 2. GET /awx_host_checker.js serves JS script
	respJS, err := http.Get(ts.URL + "/awx_host_checker.js")
	if err != nil {
		t.Fatal(err)
	}
	defer respJS.Body.Close()
	if respJS.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /awx_host_checker.js, got %d", respJS.StatusCode)
	}
	bodyJS, _ := io.ReadAll(respJS.Body)
	if len(bodyJS) < 5000 || !strings.Contains(string(bodyJS), "awxValidatorState") {
		t.Errorf("Expected awxValidatorState in checker JS, length was %d", len(bodyJS))
	}

	// 3. GET /api/health
	respHealth, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer respHealth.Body.Close()
	if respHealth.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /api/health, got %d", respHealth.StatusCode)
	}
	var healthMap map[string]interface{}
	_ = json.NewDecoder(respHealth.Body).Decode(&healthMap)
	if healthMap["status"] != "ok" {
		t.Errorf("Expected status=ok, got: %v", healthMap)
	}

	// 4. POST /api/ping
	respPing, err := http.Post(ts.URL+"/api/ping", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer respPing.Body.Close()
	if respPing.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /api/ping, got %d", respPing.StatusCode)
	}

	// 5. POST /api/bye
	respBye, err := http.Post(ts.URL+"/api/bye", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer respBye.Body.Close()
	if respBye.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /api/bye, got %d", respBye.StatusCode)
	}
}

// TestAwxInventoryHostsAPI verifies the Go backend AWX proxy and cache endpoint.
func TestAwxInventoryHostsAPI(t *testing.T) {
	_, ts := setupTestServer(t)

	// Fetch AWX inventory #3 hosts
	resp, err := http.Get(ts.URL + "/api/awx/hosts?inventory=3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from /api/awx/hosts, got %d", resp.StatusCode)
	}

	var data struct {
		Count         int                 `json:"count"`
		RawCount      int                 `json:"rawCount"`
		InventoryId   string              `json:"inventoryId"`
		Hosts         []string            `json:"hosts"`
		ExcludedHosts []string            `json:"excludedHosts"`
		Results       []map[string]string `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}

	if data.Count != 1000 || data.RawCount != 1000 {
		t.Errorf("Expected 1000 hosts from inventory 3, got count=%d raw=%d", data.Count, data.RawCount)
	}
	if len(data.Results) != 1000 {
		t.Errorf("Expected 1000 results items, got %d", len(data.Results))
	}

	// Test exclusion filter: exclude prd-db-0295 through prd-db-0300 (6 hosts)
	pipe := "prd-db-0295|prd-db-0296|prd-db-0297|prd-db-0298|prd-db-0299|prd-db-0300"
	respEx, err := http.Get(ts.URL + "/api/awx/hosts?inventory=3&exclude=" + url.QueryEscape(pipe))
	if err != nil {
		t.Fatal(err)
	}
	defer respEx.Body.Close()

	var dataEx struct {
		Count         int      `json:"count"`
		RawCount      int      `json:"rawCount"`
		Hosts         []string `json:"hosts"`
		ExcludedHosts []string `json:"excludedHosts"`
	}
	_ = json.NewDecoder(respEx.Body).Decode(&dataEx)
	if dataEx.Count != 994 || len(dataEx.ExcludedHosts) != 6 {
		t.Errorf("Expected 994 count and 6 excluded, got count=%d, excluded=%d", dataEx.Count, len(dataEx.ExcludedHosts))
	}
}

// TestWindowPinAndFlashEndpoints verifies /api/window/pin and /api/window/flash JSON contracts.
func TestWindowPinAndFlashEndpoints(t *testing.T) {
	_, ts := setupTestServer(t)

	// GET /api/window/pin
	respGet, err := http.Get(ts.URL + "/api/window/pin")
	if err != nil {
		t.Fatal(err)
	}
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on GET /api/window/pin, got %d", respGet.StatusCode)
	}
	var pinGet struct {
		Found  bool `json:"found"`
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(respGet.Body).Decode(&pinGet); err != nil {
		t.Fatal(err)
	}

	// POST /api/window/pin (pin=true)
	respPostTrue, err := http.Post(ts.URL+"/api/window/pin", "application/json", strings.NewReader(`{"pinned":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer respPostTrue.Body.Close()
	if respPostTrue.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on POST /api/window/pin, got %d", respPostTrue.StatusCode)
	}
	var pinPost struct {
		Found     bool `json:"found"`
		Pinned    bool `json:"pinned"`
		Requested bool `json:"requested"`
	}
	if err := json.NewDecoder(respPostTrue.Body).Decode(&pinPost); err != nil {
		t.Fatal(err)
	}
	if !pinPost.Requested {
		t.Errorf("Expected requested=true, got false")
	}

	// Restore pin=false
	respPostFalse, err := http.Post(ts.URL+"/api/window/pin", "application/json", strings.NewReader(`{"pinned":false}`))
	if err != nil {
		t.Fatal(err)
	}
	defer respPostFalse.Body.Close()
	if respPostFalse.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on POST /api/window/pin unpin, got %d", respPostFalse.StatusCode)
	}

	// Invalid method returns 405
	reqDel, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/window/pin", nil)
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatal(err)
	}
	defer respDel.Body.Close()
	if respDel.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 on DELETE /api/window/pin, got %d", respDel.StatusCode)
	}

	// POST /api/window/flash
	respFlash, err := http.Post(ts.URL+"/api/window/flash", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer respFlash.Body.Close()
	if respFlash.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on POST /api/window/flash, got %d", respFlash.StatusCode)
	}
}

// TestAwxConfigTestAndTLSBearer verifies pasted URL parsing, Bearer auth, self-signed TLS bypass, and scoped/paged queries.
func TestAwxConfigTestAndTLSBearer(t *testing.T) {
	_, ts := setupTestServer(t)

	tlsAWX := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-awx-tok" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/groups/12/all_hosts/":
			page := r.URL.Query().Get("page")
			if page == "2" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"count": 2,
					"next":  nil,
					"results": []map[string]string{{"name": "grp12-p2-01"}, {"name": "grp12-p2-02"}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"count":   2,
				"next":    nil,
				"results": []map[string]string{{"name": "grp12-01"}, {"name": "grp12-02"}},
			})
		case "/api/v2/inventories/9/hosts/":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"count":   2,
				"next":    nil,
				"results": []map[string]string{{"name": "inv9-01"}, {"name": "inv9-vip-02"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer tlsAWX.Close()

	// 1. POST /api/awx/config with pasted AWX browser URL & Bearer token
	pastedURL := tlsAWX.URL + "/#/inventories/inventory/9/groups/12/nested_hosts?page=2"
	cfgBody, _ := json.Marshal(map[string]interface{}{
		"baseUrl":     pastedURL,
		"token":       "secret-awx-tok",
		"insecureTls": true,
	})
	respCfg, err := http.Post(ts.URL+"/api/awx/config", "application/json", bytes.NewReader(cfgBody))
	if err != nil {
		t.Fatal(err)
	}
	defer respCfg.Body.Close()
	if respCfg.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on POST /api/awx/config, got %d", respCfg.StatusCode)
	}

	// 2. GET /api/awx/config must reflect parsed baseUrl/scope and redact secret values
	respGet, err := http.Get(ts.URL + "/api/awx/config")
	if err != nil {
		t.Fatal(err)
	}
	defer respGet.Body.Close()
	rawGetBytes, _ := io.ReadAll(respGet.Body)
	if strings.Contains(string(rawGetBytes), "secret-awx-tok") {
		t.Fatalf("GET /api/awx/config leaked raw Bearer token in response: %s", string(rawGetBytes))
	}
	var gotCfg struct {
		BaseURL      string `json:"baseUrl"`
		DefaultScope string `json:"defaultScope"`
		HasToken     bool   `json:"hasToken"`
		HasPassword  bool   `json:"hasPassword"`
		InsecureTLS  bool   `json:"insecureTls"`
	}
	if err := json.Unmarshal(rawGetBytes, &gotCfg); err != nil {
		t.Fatal(err)
	}
	if gotCfg.BaseURL != tlsAWX.URL || gotCfg.DefaultScope != "group:12" || !gotCfg.HasToken || gotCfg.HasPassword || !gotCfg.InsecureTLS {
		t.Fatalf("Unexpected parsed config: %+v", gotCfg)
	}

	// 3. POST /api/awx/test verifies live TLS + Bearer connectivity
	respTest, err := http.Post(ts.URL+"/api/awx/test", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer respTest.Body.Close()
	if respTest.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(respTest.Body)
		t.Fatalf("Expected 200 on POST /api/awx/test, got %d: %s", respTest.StatusCode, string(b))
	}
	var testRes struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.NewDecoder(respTest.Body).Decode(&testRes); err != nil {
		t.Fatal(err)
	}
	if !testRes.OK || testRes.Count != 2 {
		t.Fatalf("Unexpected test result: %+v", testRes)
	}

	// 4. GET /api/awx/hosts?scope=group:12&page=2&limit=5 fetches exact page
	respP2, err := http.Get(ts.URL + "/api/awx/hosts?scope=group:12&page=2&limit=5")
	if err != nil {
		t.Fatal(err)
	}
	defer respP2.Body.Close()
	var p2Data struct {
		Hosts []string `json:"hosts"`
	}
	if err := json.NewDecoder(respP2.Body).Decode(&p2Data); err != nil {
		t.Fatal(err)
	}
	if len(p2Data.Hosts) != 2 || p2Data.Hosts[0] != "grp12-p2-01" {
		t.Fatalf("Expected page 2 group:12 hosts, got: %+v", p2Data.Hosts)
	}

	// 5. GET /api/awx/hosts?scope=inventory:9&exclude=vip filters cleanly
	respInv9, err := http.Get(ts.URL + "/api/awx/hosts?scope=inventory:9&exclude=vip&refresh=true")
	if err != nil {
		t.Fatal(err)
	}
	defer respInv9.Body.Close()
	var inv9Data struct {
		Hosts         []string `json:"hosts"`
		ExcludedHosts []string `json:"excludedHosts"`
	}
	if err := json.NewDecoder(respInv9.Body).Decode(&inv9Data); err != nil {
		t.Fatal(err)
	}
	if len(inv9Data.Hosts) != 1 || inv9Data.Hosts[0] != "inv9-01" || len(inv9Data.ExcludedHosts) != 1 {
		t.Fatalf("Expected filtered inventory:9 hosts, got: %+v", inv9Data)
	}
}

// TestAwxBridgeCollectFlow verifies HTTP fallback cache & WebSocket live bridge tab collection.
func TestAwxBridgeCollectFlow(t *testing.T) {
	_, ts := setupTestServer(t)

	// 1. Cached page mode fallback when only HTTP state was pushed
	stateBody, _ := json.Marshal(map[string]interface{}{
		"url":       "https://awx.example.local/#/inventories/inventory/7/hosts?page=1",
		"label":     "인벤토리 #7",
		"pageHosts": []string{"web-01", "svc-vip-02", "db-03"},
	})
	respSt, err := http.Post(ts.URL+"/api/awx/bridge/state", "application/json", bytes.NewReader(stateBody))
	if err != nil {
		t.Fatal(err)
	}
	respSt.Body.Close()

	respPageCol, err := http.Post(ts.URL+"/api/awx/bridge/collect", "application/json", strings.NewReader(`{"mode":"page","exclude":"svc"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer respPageCol.Body.Close()
	if respPageCol.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on bridge cache page collect, got %d", respPageCol.StatusCode)
	}
	var pageCol struct {
		Source        string   `json:"source"`
		Hosts         []string `json:"hosts"`
		ExcludedHosts []string `json:"excludedHosts"`
	}
	if err := json.NewDecoder(respPageCol.Body).Decode(&pageCol); err != nil {
		t.Fatal(err)
	}
	if pageCol.Source != "browser-bridge-cache" || len(pageCol.Hosts) != 2 || len(pageCol.ExcludedHosts) != 1 {
		t.Fatalf("Unexpected cached page collect result: %+v", pageCol)
	}

	// 2. Connect WS bridge (?role=bridge) and complete live AWX_BRIDGE_COMMAND roundtrip
	u, _ := url.Parse(ts.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	wsReq := "GET /ws?role=bridge HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(wsReq)); err != nil {
		t.Fatal(err)
	}
	bufr := bufio.NewReader(conn)
	statusLine, err := bufr.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		t.Fatalf("Expected 101 Switching Protocols for bridge WS, got: %s", statusLine)
	}
	for {
		line, err := bufr.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// Verify /api/awx/bridge/state reports bridgeCount >= 1
	respGetBrg, err := http.Get(ts.URL + "/api/awx/bridge/state")
	if err != nil {
		t.Fatal(err)
	}
	var brgSnap struct {
		Connected   bool `json:"connected"`
		BridgeCount int  `json:"bridgeCount"`
	}
	_ = json.NewDecoder(respGetBrg.Body).Decode(&brgSnap)
	respGetBrg.Body.Close()
	if !brgSnap.Connected || brgSnap.BridgeCount < 1 {
		t.Fatalf("Expected connected bridge with count >= 1, got: %+v", brgSnap)
	}

	type colResp struct {
		Status int
		Body   map[string]interface{}
		Err    error
	}
	doneCh := make(chan colResp, 1)
	go func() {
		r, e := http.Post(ts.URL+"/api/awx/bridge/collect", "application/json", strings.NewReader(`{"mode":"all","exclude":"vip"}`))
		if e != nil {
			doneCh <- colResp{Err: e}
			return
		}
		defer r.Body.Close()
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		doneCh <- colResp{Status: r.StatusCode, Body: m}
	}()

	// Read AWX_BRIDGE_COMMAND from WS
	var cmdReqID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		op, payload, rErr := wsReadMessage(bufr)
		if rErr != nil {
			t.Fatalf("Failed reading WS command frame: %v", rErr)
		}
		if op != 0x01 {
			continue
		}
		var ev struct {
			Event string `json:"event"`
			Data  struct {
				RequestID string `json:"requestId"`
				Mode      string `json:"mode"`
			} `json:"data"`
		}
		if json.Unmarshal(payload, &ev) == nil && ev.Event == "AWX_BRIDGE_COMMAND" {
			cmdReqID = ev.Data.RequestID
			break
		}
	}
	if cmdReqID == "" {
		t.Fatalf("Did not receive AWX_BRIDGE_COMMAND over bridge WS")
	}

	// Send result back over WS RPC -> POST /api/awx/bridge/result
	rpcRes, _ := json.Marshal(map[string]interface{}{
		"id":     "res-1",
		"method": "POST",
		"path":   "/api/awx/bridge/result",
		"body": map[string]interface{}{
			"requestId": cmdReqID,
			"ok":        true,
			"hosts":     []string{"app-01", "app-vip-01", "app-02"},
			"rawCount":  3,
			"label":     "인벤토리 #7",
		},
	})
	if _, err := conn.Write(makeWSClientFrame(0x01, rpcRes)); err != nil {
		t.Fatal(err)
	}

	gotCol := <-doneCh
	if gotCol.Err != nil {
		t.Fatal(gotCol.Err)
	}
	if gotCol.Status != http.StatusOK || gotCol.Body["source"] != "browser-bridge" {
		t.Fatalf("Unexpected bridge collect response: %+v", gotCol)
	}
	if int(gotCol.Body["count"].(float64)) != 2 || int(gotCol.Body["rawCount"].(float64)) != 3 {
		t.Fatalf("Expected filtered count=2 & rawCount=3, got: %+v", gotCol.Body)
	}
}

// TestAwxSessionLoginRedirectAndSchemeFallback verifies:
// 1. Scheme-less URL parsing ("awxhost/#/inventories/inventory/33/hosts")
// 2. HTTP->HTTPS redirect preserving Authorization
// 3. Fallback from Basic Auth (401/WAF/disabled) to AWX Django /api/login/ Cookie+CSRF Session Login
// 4. Automatic https -> http scheme fallback when protocol is omitted
// 5. SPKI probe + launcher AWX certificate bypass CLI arguments
func TestAwxSessionLoginRedirectAndSchemeFallback(t *testing.T) {
	// 1. Scheme-less parsing
	b, sc, pg, exp := parseAwxTargetInputEx("awxhost/#/inventories/inventory/33/hosts?page=2")
	if b != "https://awxhost" || sc != "inventory:33" || pg != 2 || exp {
		t.Fatalf("Unexpected parseAwxTargetInputEx result: base=%q scope=%q page=%d explicit=%v", b, sc, pg, exp)
	}
	b2, sc2, pg2, exp2 := parseAwxTargetInputEx("http://awxhost/#/inventories/smart_inventory/18/hosts")
	if b2 != "http://awxhost" || sc2 != "smart_inventory:18" || pg2 != 0 || !exp2 {
		t.Fatalf("Unexpected explicit parseAwxTargetInputEx result: base=%q scope=%q page=%d explicit=%v", b2, sc2, pg2, exp2)
	}

	// 2. Mock HTTPS AWX server requiring Web Session (/api/login/) + blocking Basic Auth & Go default UA
	tlsSessionAWX := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Go-http-client") {
			http.Error(w, "WAF blocked Go UA", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/api/login/":
			if r.Method == http.MethodGet {
				http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf-cookie-123", Path: "/"})
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = io.WriteString(w, `<form method="post" action="/api/login/"><input type="hidden" name="csrfmiddlewaretoken" value="csrf-form-456"><input type="text" name="username"><input type="password" name="password"></form>`)
				return
			}
			if r.Method == http.MethodPost {
				c, err := r.Cookie("csrftoken")
				if err != nil || c.Value != "csrf-cookie-123" {
					http.Error(w, "CSRF cookie missing", http.StatusForbidden)
					return
				}
				if !strings.HasPrefix(r.Header.Get("Referer"), "https://") || r.Header.Get("Origin") == "" {
					http.Error(w, "CSRF Origin/Referer missing", http.StatusForbidden)
					return
				}
				_ = r.ParseForm()
				if r.FormValue("csrfmiddlewaretoken") != "csrf-form-456" {
					http.Error(w, "CSRF form token mismatch", http.StatusForbidden)
					return
				}
				if r.FormValue("username") != "corp.user" || r.FormValue("password") != "CorpPass!1" {
					w.Header().Set("Content-Type", "text/html")
					_, _ = io.WriteString(w, `<div>Please enter a correct username and password.</div>`)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "awx_sessionid", Value: "sess-ok-999", Path: "/"})
				http.Redirect(w, r, "/api/v2/", http.StatusFound)
				return
			}
		case "/api/v2/inventories/33/hosts/":
			// Reject Basic Auth to simulate enterprise AWX disabling Basic Auth
			if r.Header.Get("Authorization") != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"detail":"HTTP Basic auth is disabled."}`)
				return
			}
			sc, err := r.Cookie("awx_sessionid")
			if err != nil || sc.Value != "sess-ok-999" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"detail":"Authentication credentials were not provided."}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"count": 4,
					"next":  nil,
					"results": []map[string]string{
						{"name": "inv33-p2-01"}, {"name": "inv33-p2-02"},
					},
				})
				return
			}
			next := "/api/v2/inventories/33/hosts/?page=2"
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"count": 4,
				"next":  &next,
				"results": []map[string]string{
					{"name": "inv33-01"}, {"name": "inv33-02"},
				},
			})
			return
		case "/api/v2/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"current_version":"24.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer tlsSessionAWX.Close()

	_, ts := setupTestServer(t)

	// Save & test HTTPS session-only AWX
	cfgBody, _ := json.Marshal(map[string]interface{}{
		"baseUrl":     tlsSessionAWX.URL + "/#/inventories/inventory/33/hosts",
		"username":    "corp.user",
		"password":    "CorpPass!1",
		"insecureTls": true,
	})
	respCfg, err := http.Post(ts.URL+"/api/awx/config", "application/json", bytes.NewReader(cfgBody))
	if err != nil {
		t.Fatal(err)
	}
	respCfg.Body.Close()

	respTest, err := http.Post(ts.URL+"/api/awx/test", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer respTest.Body.Close()
	rawTest, _ := io.ReadAll(respTest.Body)
	if respTest.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on session AWX test, got %d: %s", respTest.StatusCode, string(rawTest))
	}
	var testOut struct {
		OK         bool   `json:"ok"`
		Count      int    `json:"count"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal(rawTest, &testOut); err != nil {
		t.Fatal(err)
	}
	if !testOut.OK || testOut.Count != 4 || testOut.AuthMethod != "session" {
		t.Fatalf("Expected ok=true count=4 authMethod=session, got: %+v", testOut)
	}

	// Collect all pages using cached session
	respCollect, err := http.Get(ts.URL + "/api/awx/hosts?scope=inventory:33&refresh=true")
	if err != nil {
		t.Fatal(err)
	}
	defer respCollect.Body.Close()
	if respCollect.StatusCode != http.StatusOK {
		bErr, _ := io.ReadAll(respCollect.Body)
		t.Fatalf("Expected 200 collecting multi-page session AWX hosts, got %d: %s", respCollect.StatusCode, string(bErr))
	}
	var collectOut struct {
		Count int      `json:"count"`
		Hosts []string `json:"hosts"`
	}
	if err := json.NewDecoder(respCollect.Body).Decode(&collectOut); err != nil {
		t.Fatal(err)
	}
	if collectOut.Count != 4 {
		t.Fatalf("Expected 4 collected hosts across 2 session pages, got %+v", collectOut)
	}

	// Verify changing scheme or IP/hostname retains saved non-default user password on config update
	updBody, _ := json.Marshal(map[string]interface{}{
		"baseUrl": tlsSessionAWX.URL + "/",
	})
	respUpd, err := http.Post(ts.URL+"/api/awx/config", "application/json", bytes.NewReader(updBody))
	if err != nil {
		t.Fatal(err)
	}
	var updOut struct {
		HasPassword bool `json:"hasPassword"`
	}
	_ = json.NewDecoder(respUpd.Body).Decode(&updOut)
	respUpd.Body.Close()
	if !updOut.HasPassword {
		t.Fatalf("Expected saved custom password to remain preserved across URL re-test")
	}

	// 3. Test HTTP -> HTTPS 301 redirect preserving Basic Auth
	redirectTarget := tlsSessionAWX.URL // reuse TLS listener or create TLS Basic server
	_ = redirectTarget
	tlsBasic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "redir.user" || p != "RedirPass!" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"count":1,"next":null,"results":[{"name":"redir-host-01"}]}`)
	}))
	defer tlsBasic.Close()

	httpRedirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsBasic.URL+r.RequestURI, http.StatusMovedPermanently)
	}))
	defer httpRedirector.Close()

	cfgRedir, _ := json.Marshal(map[string]interface{}{
		"baseUrl":      httpRedirector.URL,
		"defaultScope": "inventory:33",
		"username":     "redir.user",
		"password":     "RedirPass!",
		"insecureTls":  true,
	})
	r1, _ := http.Post(ts.URL+"/api/awx/config", "application/json", bytes.NewReader(cfgRedir))
	r1.Body.Close()
	r2, err := http.Post(ts.URL+"/api/awx/test", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(r2.Body)
		t.Fatalf("Expected HTTP->HTTPS redirect preserving Basic Auth to succeed, got %d: %s", r2.StatusCode, string(rb))
	}

	// 4. Test scheme-less rawUrl falling back from https:// to http:// when target is HTTP-only
	httpOnlyAWX := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "plain.user" || p != "PlainPass!" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"count":1,"next":null,"results":[{"name":"plain-host-01"}]}`)
	}))
	defer httpOnlyAWX.Close()
	httpHostPort := strings.TrimPrefix(httpOnlyAWX.URL, "http://")

	rawInput := httpHostPort + "/#/inventories/inventory/33/hosts"
	cfgPlain, _ := json.Marshal(map[string]interface{}{
		"baseUrl":     rawInput,
		"username":    "plain.user",
		"password":    "PlainPass!",
		"insecureTls": true,
	})
	rp1, _ := http.Post(ts.URL+"/api/awx/config", "application/json", bytes.NewReader(cfgPlain))
	rp1.Body.Close()
	testFallbackBody, _ := json.Marshal(map[string]interface{}{
		"rawUrl":              rawInput,
		"allowSchemeFallback": true,
	})
	rp2, err := http.Post(ts.URL+"/api/awx/test", "application/json", bytes.NewReader(testFallbackBody))
	if err != nil {
		t.Fatal(err)
	}
	defer rp2.Body.Close()
	rpBytes, _ := io.ReadAll(rp2.Body)
	if rp2.StatusCode != http.StatusOK {
		t.Fatalf("Expected https->http auto fallback to succeed, got %d: %s", rp2.StatusCode, string(rpBytes))
	}
	var rpOut struct {
		BaseURL string `json:"baseUrl"`
	}
	_ = json.Unmarshal(rpBytes, &rpOut)
	if rpOut.BaseURL != httpOnlyAWX.URL {
		t.Fatalf("Expected resolved baseUrl=%q, got %q", httpOnlyAWX.URL, rpOut.BaseURL)
	}

	// 5. Verify SPKI probe & launcher AWX args
	hashes := probeServerSPKIHashes(tlsSessionAWX.URL, 2*time.Second)
	if len(hashes) == 0 {
		t.Fatalf("Expected non-empty SPKI hashes from TLS test server")
	}
	sum := sha256.Sum256(tlsSessionAWX.Certificate().RawSubjectPublicKeyInfo)
	expectedHash := base64.StdEncoding.EncodeToString(sum[:])
	if hashes[0] != expectedHash {
		t.Fatalf("SPKI hash mismatch: got %q, want %q", hashes[0], expectedHash)
	}
	argsSPKI := launcher.BuildAWXArgs("https://awxhost/#/inventories/inventory/33/hosts", "C:\\tmp\\awxprof", hashes, true)
	joinedSPKI := strings.Join(argsSPKI, " ")
	if !strings.Contains(joinedSPKI, "--ignore-certificate-errors-spki-list="+expectedHash) ||
		!strings.Contains(joinedSPKI, "BlockInsecurePrivateNetworkRequests") ||
		!strings.Contains(joinedSPKI, "--new-window") {
		t.Fatalf("Unexpected BuildAWXArgs output: %v", argsSPKI)
	}
	argsFallback := launcher.BuildAWXArgs("https://awxhost", "C:\\tmp\\awxprof", nil, true)
	if !strings.Contains(strings.Join(argsFallback, " "), "--ignore-certificate-errors") {
		t.Fatalf("Expected --ignore-certificate-errors fallback when SPKI empty: %v", argsFallback)
	}
}

func TestAwxSessionEdgeCasesAndFailureDiagnostics(t *testing.T) {
	srv := &AgentServer{}

	// 1. Navbar <form action="/search/"> before real login form + missing csrftoken on GET /api/login/ seeded from /api/v2/
	var sawTenant, sawResyncedCSRF string
	awxMultiForm := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/inventories/33/hosts/" {
			if c, err := r.Cookie("awx_sessionid"); err == nil && c.Value == "sess-edge-1" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"count":1,"next":null,"results":[{"name":"edge-host-01"}]}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Authentication credentials were not provided. To establish a login session, visit /api/login/."}`)
			return
		}
		if r.URL.Path == "/api/v2/" && r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "seeded-csrf-777", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			if c, err := r.Cookie("awx_sessionid"); err == nil && c.Value == "sess-edge-1" {
				_, _ = io.WriteString(w, `{"description":"AWX REST API"}`)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"seed csrftoken"}`)
			return
		}
		if r.URL.Path == "/api/login/" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			tok := "stale-before-cookie"
			if c, err := r.Cookie("csrftoken"); err == nil && c.Value == "seeded-csrf-777" {
				tok = "synced-csrf-777"
			}
			_, _ = fmt.Fprintf(w, `<html><body>
				<form action="/search/" method="get"><input type="text" name="q"></form>
				<form action="/api/login/" method="post">
					<input type="hidden" name="csrfmiddlewaretoken" value="%s">
					<input type="hidden" name="tenant" value="prod-kr">
					<input type="text" name="username">
					<input type="password" name="password">
				</form></body></html>`, tok)
			return
		}
		if r.URL.Path == "/api/login/" && r.Method == http.MethodPost {
			_ = r.ParseForm()
			csrfCook, _ := r.Cookie("csrftoken")
			sawTenant = r.FormValue("tenant")
			sawResyncedCSRF = r.FormValue("csrfmiddlewaretoken")
			if csrfCook == nil || csrfCook.Value != "seeded-csrf-777" || sawResyncedCSRF != "synced-csrf-777" || sawTenant != "prod-kr" {
				http.Error(w, "CSRF or form mismatch", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "awx_sessionid", Value: "sess-edge-1", Path: "/", HttpOnly: true})
			http.Redirect(w, r, "/api/v2/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer awxMultiForm.Close()

	pg, mode, _, err := srv.fetchAwxPageWithDetails(awxMultiForm.URL+"/api/v2/inventories/33/hosts/", "admin", "Secret!", "", true, nil)
	if err != nil {
		t.Fatalf("Expected multi-form/seeded-csrf login to succeed, got error: %v", err)
	}
	if mode != "session" || pg == nil || len(pg.Results) != 1 || pg.Results[0].Name != "edge-host-01" {
		t.Fatalf("Unexpected result: mode=%q pg=%+v", mode, pg)
	}
	if sawTenant != "prod-kr" || sawResyncedCSRF != "synced-csrf-777" {
		t.Fatalf("Expected hidden field and resynced CSRF token, got tenant=%q csrf=%q", sawTenant, sawResyncedCSRF)
	}

	// 2. SPA HTML on /api/login/ (no password input) falling through to JSON /api/gateway/v1/login/
	awxGateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/inventories/33/hosts/":
			if c, err := r.Cookie("awx_sessionid"); err == nil && c.Value == "gw-sess-9" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"count":1,"next":null,"results":[{"name":"gw-host-01"}]}`)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Authentication credentials were not provided."}`)
		case "/api/login/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<!doctype html><html><title>AWX UI</title><body><div id="root"></div></body></html>`)
		case "/api/gateway/v1/login/":
			if r.Method == http.MethodGet {
				http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "gw-csrf", Path: "/"})
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"detail":"POST username and password"}`)
				return
			}
			var creds map[string]string
			_ = json.NewDecoder(r.Body).Decode(&creds)
			if creds["username"] == "gwuser" && creds["password"] == "GwPass!" {
				http.SetCookie(w, &http.Cookie{Name: "awx_sessionid", Value: "gw-sess-9", Path: "/"})
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"ok"}`)
				return
			}
			http.Error(w, `{"detail":"Bad credentials"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer awxGateway.Close()

	pgGW, modeGW, _, errGW := srv.fetchAwxPageWithDetails(awxGateway.URL+"/api/v2/inventories/33/hosts/", "gwuser", "GwPass!", "", true, nil)
	if errGW != nil || modeGW != "session" || len(pgGW.Results) != 1 || pgGW.Results[0].Name != "gw-host-01" {
		t.Fatalf("Expected gateway JSON fallback login to succeed, got pg=%+v mode=%q err=%v", pgGW, modeGW, errGW)
	}

	// 3. Re-rendered login form with custom error (no session cookie) must NOT mask behind subsequent HTTP 401
	awxSilentFail := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/inventories/33/hosts/":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Authentication credentials were not provided. To establish a login session, visit /api/login/."}`)
		case "/api/login/":
			if r.Method == http.MethodGet {
				http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "c1", Path: "/"})
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<form method="post" action="/api/login/"><input type="hidden" name="csrfmiddlewaretoken" value="c1"><input type="text" name="username"><input type="password" name="password"></form>`)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<form method="post" action="/api/login/"><div class="error">LDAP bind failed for user</div><input type="text" name="username"><input type="password" name="password"></form>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer awxSilentFail.Close()

	_, _, _, errFail := srv.fetchAwxPageWithDetails(awxSilentFail.URL+"/api/v2/inventories/33/hosts/", "alice", "Wrong!", "", true, nil)
	if errFail == nil || strings.Contains(errFail.Error(), "Authentication credentials were not provided") || !strings.Contains(errFail.Error(), "LDAP bind failed") {
		t.Fatalf("Expected diagnostic login failure message containing LDAP error instead of masked 401, got: %v", errFail)
	}

	// 4. Reverse proxy Set-Cookie Domain mismatch causing CookieJar to drop session cookie must report Set-Cookie diagnostic
	awxBadDomainCookie := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/inventories/33/hosts/":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"detail":"Authentication credentials were not provided. To establish a login session, visit /api/login/."}`)
		case "/api/login/":
			if r.Method == http.MethodGet {
				http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "c1", Path: "/"})
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<form method="post" action="/api/login/"><input type="hidden" name="csrfmiddlewaretoken" value="c1"><input type="text" name="username"><input type="password" name="password"></form>`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "awx_sessionid", Value: "dropped", Domain: "other-internal.corp.local", Path: "/"})
			http.Redirect(w, r, "/api/v2/", http.StatusFound)
		case "/api/v2/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"description":"AWX API"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer awxBadDomainCookie.Close()

	_, _, _, errCookie := srv.fetchAwxPageWithDetails(awxBadDomainCookie.URL+"/api/v2/inventories/33/hosts/", "alice", "GoodPass!", "", true, nil)
	if errCookie == nil || !strings.Contains(errCookie.Error(), "세션 쿠키가 저장되지 않았습니다") || !strings.Contains(errCookie.Error(), "other-internal.corp.local") {
		t.Fatalf("Expected Set-Cookie Domain mismatch diagnostic, got: %v", errCookie)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
