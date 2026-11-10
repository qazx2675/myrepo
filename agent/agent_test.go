package main

import (
	"bufio"
	"bytes"
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
