// Command server is the VM 자동생성 웹 포털's HTTP entrypoint (M1~M4 scope):
// login/session/RBAC, encrypted credential storage, and Phase 1~3
// (host register / vSwitch / VM create) wrapping the existing govmomi binaries.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"vm-portal/internal/auth"
	"vm-portal/internal/config"
	dbpkg "vm-portal/internal/db"
	"vm-portal/internal/models"
	"vm-portal/internal/phases"
	"vm-portal/internal/secrets"
	"vm-portal/internal/webui"
)

// Every page template defines a block named "content" (see base.html's
// {{template "content" .}}), so they can't be parsed together with one
// glob — the last one parsed would silently win and clobber the rest.
// Instead each page gets its own {base.html + page.html} template set.
var pageNames = []string{"login", "dashboard", "credentials", "phase1", "phase2", "phase3", "phase4", "phase5", "phase6", "phase7", "phase8", "phase9", "pipeline", "pipeline_result", "job_result", "audit"}

var tmplSet map[string]*template.Template

func loadTemplates() map[string]*template.Template {
	set := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		set[name] = template.Must(template.New("base").ParseFS(webui.FS, "templates/base.html", "templates/"+name+".html"))
	}
	return set
}

type server struct {
	db  *sql.DB
	cfg config.Config
	key []byte
}

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.TmpDir, 0700); err != nil {
		log.Fatalf("tmp 디렉터리 생성 실패: %v", err)
	}
	if err := os.MkdirAll(dirOf(cfg.DBPath), 0700); err != nil {
		log.Fatalf("data 디렉터리 생성 실패: %v", err)
	}
	if err := os.MkdirAll(cfg.ReportsDir, 0700); err != nil {
		log.Fatalf("리포트 디렉터리 생성 실패: %v", err)
	}

	database, err := dbpkg.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("DB 열기 실패: %v", err)
	}
	defer database.Close()

	key, err := secrets.LoadMasterKey(cfg.SecretBackend, cfg.MasterKeyFile, cfg.VaultAddr, cfg.VaultToken, cfg.VaultSecretPath, cfg.VaultKeyField)
	if err != nil {
		log.Fatalf("마스터 키 로드 실패 (backend=%s): %v", cfg.SecretBackend, err)
	}

	if err := bootstrapAdmin(database); err != nil {
		log.Fatalf("관리자 계정 부트스트랩 실패: %v", err)
	}

	tmplSet = loadTemplates()

	s := &server{db: database, cfg: cfg, key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /{$}", auth.RequireLevel(models.LevelViewer, s.handleDashboard))
	mux.HandleFunc("GET /credentials", auth.RequireLevel(models.LevelViewer, s.handleCredentialsGet))
	mux.HandleFunc("POST /credentials", auth.RequireLevel(models.LevelAdmin, s.handleCredentialsPost))
	mux.HandleFunc("GET /phase1", auth.RequireLevel(models.LevelOperator, s.handlePhase1Get))
	mux.HandleFunc("POST /phase1", auth.RequireLevel(models.LevelOperator, s.handlePhase1Post))
	mux.HandleFunc("GET /phase2", auth.RequireLevel(models.LevelOperator, s.handlePhase2Get))
	mux.HandleFunc("POST /phase2", auth.RequireLevel(models.LevelOperator, s.handlePhase2Post))
	mux.HandleFunc("GET /phase3", auth.RequireLevel(models.LevelOperator, s.handlePhase3Get))
	mux.HandleFunc("POST /phase3", auth.RequireLevel(models.LevelOperator, s.handlePhase3Post))
	mux.HandleFunc("GET /phase4", auth.RequireLevel(models.LevelOperator, s.handlePhase4Get))
	mux.HandleFunc("POST /phase4", auth.RequireLevel(models.LevelOperator, s.handlePhase4Post))
	mux.HandleFunc("GET /phase5", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase5Get))
	mux.HandleFunc("POST /phase5", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase5Post))
	mux.HandleFunc("GET /phase6", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase6Get))
	mux.HandleFunc("POST /phase6", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase6Post))
	mux.HandleFunc("GET /phase7", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase7Get))
	mux.HandleFunc("POST /phase7", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase7Post))
	mux.HandleFunc("GET /phase8", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase8Get))
	mux.HandleFunc("POST /phase8", auth.RequireLevel(models.LevelSeniorOperator, s.handlePhase8Post))
	mux.HandleFunc("GET /phase9", auth.RequireLevel(models.LevelViewer, s.handlePhase9Get))
	mux.HandleFunc("POST /phase9", auth.RequireLevel(models.LevelViewer, s.handlePhase9Post))
	mux.HandleFunc("GET /phase9/download/{jobID}", auth.RequireLevel(models.LevelViewer, s.handlePhase9Download))
	mux.HandleFunc("GET /pipeline", auth.RequireLevel(models.LevelOperator, s.handlePipelineGet))
	mux.HandleFunc("POST /pipeline", auth.RequireLevel(models.LevelOperator, s.handlePipelinePost))
	mux.HandleFunc("GET /audit", auth.RequireLevel(models.LevelAdmin, s.handleAuditGet))

	handler := auth.WithUser(database)(mux)

	log.Printf("VM 포털 시작: %s (bin=%s tmp=%s db=%s)", cfg.ListenAddr, cfg.BinDir, cfg.TmpDir, cfg.DBPath)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, handler))
}

func dirOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "."
	}
	return path[:i]
}

// bootstrapAdmin creates a Level 5 "admin" user on first run if the users
// table is empty, using VMPORTAL_BOOTSTRAP_PASSWORD (or a random one printed
// once) so there is always a way in on a fresh DB.
func bootstrapAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	pw := os.Getenv("VMPORTAL_BOOTSTRAP_PASSWORD")
	generated := false
	if pw == "" {
		pw = randomPassword()
		generated = true
	}

	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, rbac_level) VALUES ('admin', ?, 5)`, hash); err != nil {
		return err
	}

	if generated {
		log.Printf("=================================================================")
		log.Printf(" 초기 관리자 계정이 생성되었습니다 - username: admin / password: %s", pw)
		log.Printf(" 로그인 후 반드시 비밀번호를 변경하세요 (현재 UI에는 변경 기능 없음 - DB 직접 수정 필요)")
		log.Printf("=================================================================")
	}
	return nil
}

func randomPassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return "changeme-please-set-VMPORTAL_BOOTSTRAP_PASSWORD"
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}

// --- auth handlers ---

func (s *server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if u, ok := auth.CurrentUser(r); ok && u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, "login", map[string]any{})
}

func (s *server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	row := s.db.QueryRow(`SELECT id, username, password_hash, rbac_level, created_at FROM users WHERE username = ?`, username)
	var u models.User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.RBACLevel, &u.CreatedAt); err != nil || !auth.CheckPassword(u.PasswordHash, password) {
		render(w, "login", map[string]any{"Error": "아이디 또는 비밀번호가 올바르지 않습니다"})
		return
	}

	token, expires, err := auth.CreateSession(s.db, u.ID)
	if err != nil {
		http.Error(w, "세션 생성 실패", http.StatusInternalServerError)
		return
	}
	auth.SetSessionCookie(w, token, expires)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("vmportal_session"); err == nil {
		_ = auth.DeleteSession(s.db, c.Value)
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- dashboard ---

func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	render(w, "dashboard", baseData(r, nil))
}

// --- credentials ---

func (s *server) handleCredentialsGet(w http.ResponseWriter, r *http.Request) {
	creds, err := secrets.ListCredentials(s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := baseData(r, map[string]any{"Credentials": creds})
	render(w, "credentials", data)
}

func (s *server) handleCredentialsPost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	ip := strings.TrimSpace(r.FormValue("vcenter_ip"))
	vcID := strings.TrimSpace(r.FormValue("vc_id"))
	vcPass := r.FormValue("vc_password")
	esxiPass := r.FormValue("esxi_password")

	extra := map[string]any{}
	if name == "" || ip == "" || vcID == "" || vcPass == "" {
		extra["Error"] = "이름/IP/계정/비밀번호는 필수입니다"
	} else if _, err := secrets.SaveCredential(s.db, s.key, name, ip, vcID, vcPass, esxiPass, user.ID); err != nil {
		extra["Error"] = "저장 실패: " + err.Error()
	} else {
		extra["OK"] = "자격증명이 저장되었습니다"
	}

	creds, _ := secrets.ListCredentials(s.db)
	extra["Credentials"] = creds
	render(w, "credentials", baseData(r, extra))
}

// --- phase 1: host register ---

func (s *server) handlePhase1Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase1", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase1Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	folder := strings.TrimSpace(r.FormValue("folder_name"))
	hosts := splitLines(r.FormValue("hosts"))

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase1", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase1(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase1Input{FolderName: folder, Hosts: hosts}, user.ID)
	s.finishJob(w, r, "phase1_host_register", jobID, res, err, user.ID, folder)
}

// --- phase 2: vswitch/portgroup ---

func (s *server) handlePhase2Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase2", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase2Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	targetVSwitch := strings.TrimSpace(r.FormValue("target_vswitch"))

	rawLines := splitLines(r.FormValue("entries"))
	var entries []phases.VSwitchEntry
	var badLines []string
	for _, line := range rawLines {
		parts := splitFields(line)
		if len(parts) < 3 {
			badLines = append(badLines, line)
			continue
		}
		vlan, err := strconv.Atoi(parts[2])
		if err != nil {
			badLines = append(badLines, line)
			continue
		}
		entries = append(entries, phases.VSwitchEntry{BMHost: parts[0], PGName: parts[1], VlanID: vlan})
	}
	if len(entries) == 0 {
		s.renderPhaseError(w, r, "phase2", "포트그룹 설정에서 유효한 항목을 찾지 못했습니다. \"호스트 포트그룹명 VLAN번호\" 형식으로 한 줄에 하나씩 입력하세요 (공백 또는 쉼표 구분).")
		return
	}
	if len(badLines) > 0 {
		s.renderPhaseError(w, r, "phase2", "일부 줄을 인식하지 못했습니다 (형식: \"호스트 포트그룹명 VLAN번호\"): "+strings.Join(badLines, " | "))
		return
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase2", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase2(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase2Input{TargetVSwitch: targetVSwitch, Entries: entries}, user.ID)
	s.finishJob(w, r, "phase2_vswitch", jobID, res, err, user.ID, targetVSwitch)
}

// --- phase 3: vm create ---

func (s *server) handlePhase3Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase3", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase3Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	datacenter := strings.TrimSpace(r.FormValue("datacenter"))
	firmware := strings.TrimSpace(r.FormValue("firmware"))
	vmCount, _ := strconv.Atoi(r.FormValue("vm_count"))
	hosts := splitLines(r.FormValue("hosts"))

	hostgroupRaw := splitLines(r.FormValue("hostgroup_map"))
	hostgroupMap := map[string]string{}
	var badHostgroupLines []string
	for _, line := range hostgroupRaw {
		parts := splitFields(line)
		if len(parts) < 2 {
			badHostgroupLines = append(badHostgroupLines, line)
			continue
		}
		hostgroupMap[parts[0]] = parts[1]
	}
	// hostgroup_map is optional (VMs can be created without a NIC), but if the
	// user typed something that failed to parse entirely, that's a mistake
	// worth surfacing rather than silently creating VMs with no network adapter.
	if len(hostgroupRaw) > 0 && len(hostgroupMap) == 0 {
		s.renderPhaseError(w, r, "phase3", "Hostgroup 매핑에서 유효한 항목을 찾지 못했습니다. \"호스트 포트그룹명\" 형식으로 한 줄에 하나씩 입력하세요 (공백 또는 쉼표 구분). 비워두면 네트워크 어댑터 없이 VM이 생성됩니다.")
		return
	}
	if len(badHostgroupLines) > 0 {
		s.renderPhaseError(w, r, "phase3", "Hostgroup 매핑에서 일부 줄을 인식하지 못했습니다: "+strings.Join(badHostgroupLines, " | "))
		return
	}

	specs := map[int]phases.VMSpec{
		1: {Cpu: formInt(r, "ev01_cpu"), Mem: formInt(r, "ev01_mem"), Disk: formInt(r, "ev01_disk"), Share: formInt(r, "ev01_share")},
		2: {Cpu: formInt(r, "ev02_cpu"), Mem: formInt(r, "ev02_mem"), Disk: formInt(r, "ev02_disk"), Share: formInt(r, "ev02_share")},
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase3", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	in := phases.Phase3Input{
		Datacenter:   datacenter,
		Firmware:     firmware,
		VMCount:      vmCount,
		Hosts:        hosts,
		HostGroupMap: hostgroupMap,
		Specs:        specs,
	}
	jobID, res, err := phases.RunPhase3(r.Context(), s.db, s.cfg, s.key, cred, in, user.ID)
	s.finishJob(w, r, "phase3_vm_create", jobID, res, err, user.ID, strings.Join(hosts, ","))
}

// --- phase 4: mac/ip extraction ---

func (s *server) handlePhase4Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase4", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase4Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))
	arg1 := strings.TrimSpace(r.FormValue("arg1"))
	argInt := formInt(r, "arg_int")
	argStr := strings.TrimSpace(r.FormValue("arg_str"))

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase4", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase4(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase4Input{Hosts: hosts, Arg1: arg1, ArgInt: argInt, ArgStr: argStr}, user.ID)
	s.finishJob(w, r, "phase4_mac_extract", jobID, res, err, user.ID, arg1)
}

// --- phase 5: cpu affinity ---

func (s *server) handlePhase5Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase5", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase5Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))

	rawPairs := splitLines(r.FormValue("affinity_pairs"))
	pairs := map[string]string{}
	var badPairs []string
	for _, line := range rawPairs {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			badPairs = append(badPairs, line)
			continue
		}
		pairs[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	if len(rawPairs) > 0 && len(pairs) == 0 {
		s.renderPhaseError(w, r, "phase5", "EV02 Affinity 설정에서 유효한 항목을 찾지 못했습니다. \"키=값\" 형식으로 한 줄에 하나씩 입력하세요. 비워두면 EV02는 건너뜁니다.")
		return
	}
	if len(badPairs) > 0 {
		s.renderPhaseError(w, r, "phase5", "EV02 Affinity 설정에서 일부 줄을 인식하지 못했습니다: "+strings.Join(badPairs, " | "))
		return
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase5", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase5(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase5Input{Hosts: hosts, AffinityPairs: pairs}, user.ID)
	s.finishJob(w, r, "phase5_affinity", jobID, res, err, user.ID, strings.Join(hosts, ","))
}

// --- phase 6: lpage/numa tuning ---

func (s *server) handlePhase6Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase6", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase6Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))

	in := phases.Phase6Input{
		Hosts:       hosts,
		EV01Cores:   formInt(r, "ev01_cores"),
		EV01Sockets: formInt(r, "ev01_sockets"),
		EV02Cores:   formInt(r, "ev02_cores"),
		EV02Sockets: formInt(r, "ev02_sockets"),
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase6", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase6(r.Context(), s.db, s.cfg, s.key, cred, in, user.ID)
	s.finishJob(w, r, "phase6_lpage", jobID, res, err, user.ID, strings.Join(hosts, ","))
}

// --- phase 7: power policy (destructive) ---

func (s *server) handlePhase7Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase7", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase7Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	dryRun := r.FormValue("dry_run") == "on"
	if r.FormValue("confirm") != "on" {
		s.renderPhaseError(w, r, "phase7", "확인 체크박스를 선택해야 실행됩니다.")
		return
	}
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase7", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase7(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase7Input{Hosts: hosts, DryRun: dryRun}, user.ID)
	s.finishJob(w, r, "phase7_power_policy", jobID, res, err, user.ID, strings.Join(hosts, ","))
}

// --- phase 8: vm delete (destructive, new development) ---

func (s *server) handlePhase8Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase8", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase8Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	dryRun := r.FormValue("dry_run") == "on"
	if !dryRun && r.FormValue("confirm_text") != "DELETE" {
		s.renderPhaseError(w, r, "phase8", "확인란에 정확히 DELETE 를 입력해야 실행됩니다.")
		return
	}
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	vmNames := splitLines(r.FormValue("vm_names"))
	if len(vmNames) == 0 {
		s.renderPhaseError(w, r, "phase8", "삭제할 VM 이름을 최소 1개 입력하세요.")
		return
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase8", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, err := phases.RunPhase8(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase8Input{VMNames: vmNames, DryRun: dryRun}, user.ID)
	s.finishJob(w, r, "phase8_vm_delete", jobID, res, err, user.ID, strings.Join(vmNames, ","))
}

// --- phase 9: report (read-only, M7) ---

func (s *server) handlePhase9Get(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "phase9", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePhase9Post(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))
	if len(hosts) == 0 {
		s.renderPhaseError(w, r, "phase9", "조회할 BM 호스트를 최소 1개 입력하세요.")
		return
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "phase9", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	jobID, res, reportFile, err := phases.RunPhase9(r.Context(), s.db, s.cfg, s.key, cred, phases.Phase9Input{Hosts: hosts}, user.ID)
	if err != nil {
		http.Error(w, "작업 실행 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}

	status := "success"
	auditResult := "success"
	if !res.Success {
		status = "failed"
		auditResult = "failed"
	}
	logAudit(s.db, user.ID, "phase9_report", strings.Join(hosts, ","), auditResult)

	data := baseData(r, map[string]any{
		"Phase":  "phase9_report",
		"JobID":  jobID,
		"Status": status,
		"Lines":  res.Lines,
	})
	if reportFile != "" {
		data["DownloadURL"] = fmt.Sprintf("/phase9/download/%d", jobID)
	}
	render(w, "job_result", data)
}

func (s *server) handlePhase9Download(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(r.PathValue("jobID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var phase, mapfile string
	err = s.db.QueryRow(`SELECT phase, mapfile FROM jobs WHERE id = ?`, jobID).Scan(&phase, &mapfile)
	if err != nil || phase != "phase9_report" || mapfile == "" {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(s.cfg.ReportsDir, mapfile)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+mapfile+"\"")
	http.ServeFile(w, r, path)
}

// --- audit log (M8, admin only) ---

type auditEntry struct {
	CreatedAt string
	Username  string
	Action    string
	Target    string
	Result    string
}

func (s *server) handleAuditGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT a.created_at, COALESCE(u.username, '(삭제된 사용자)'), a.action, COALESCE(a.target, ''), a.result
		FROM audit_log a
		LEFT JOIN users u ON u.id = a.user_id
		ORDER BY a.id DESC
		LIMIT 200`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(&e.CreatedAt, &e.Username, &e.Action, &e.Target, &e.Result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, e)
	}

	render(w, "audit", baseData(r, map[string]any{"Entries": entries}))
}

// --- pipeline: run phase 1~6 in sequence from one page ---

type pipelineStep struct {
	Name        string
	JobID       int64
	Status      string
	StatusClass string
	Lines       []string
	ErrorMsg    string
}

func (s *server) handlePipelineGet(w http.ResponseWriter, r *http.Request) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, "pipeline", baseData(r, map[string]any{"Credentials": creds}))
}

func (s *server) handlePipelinePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r)
	credID, _ := strconv.ParseInt(r.FormValue("credential_id"), 10, 64)
	hosts := splitLines(r.FormValue("hosts"))
	stopOnFail := r.FormValue("stop_on_fail") == "on"

	// Phase 5/6 are RBAC level 4+ individually; the /pipeline route itself
	// only requires level 3 (it also covers phase 1~3), so a level-3 user
	// enabling those checkboxes here must not bypass that gate.
	if (r.FormValue("enable_phase5") == "on" || r.FormValue("enable_phase6") == "on") && user.RBACLevel < models.LevelSeniorOperator {
		s.renderPhaseError(w, r, "pipeline", "Phase 5/6은 Lv.4 이상만 실행할 수 있습니다.")
		return
	}

	cred, err := secrets.GetDecrypted(s.db, s.key, credID)
	if err != nil {
		s.renderPhaseError(w, r, "pipeline", "자격증명을 불러올 수 없습니다: "+err.Error())
		return
	}

	var steps []pipelineStep
	failed := false

	runStep := func(name string, jobID int64, res *phases.Result, runErr error) bool {
		step := pipelineStep{Name: name, JobID: jobID}
		switch {
		case runErr != nil:
			step.Status = "오류"
			step.StatusClass = "failed"
			step.ErrorMsg = runErr.Error()
			failed = true
		case !res.Success:
			step.Status = "실패"
			step.StatusClass = "failed"
			step.Lines = res.Lines
			failed = true
		default:
			step.Status = "성공"
			step.StatusClass = "success"
			step.Lines = res.Lines
		}
		steps = append(steps, step)
		return !failed
	}

	ctx := r.Context()

	if r.FormValue("enable_phase1") == "on" {
		folder := strings.TrimSpace(r.FormValue("folder_name"))
		jobID, res, runErr := phases.RunPhase1(ctx, s.db, s.cfg, s.key, cred, phases.Phase1Input{FolderName: folder, Hosts: hosts}, user.ID)
		if ok := runStep("Phase 1: 호스트 등록", jobID, res, runErr); !ok && stopOnFail {
			s.renderPipelineResult(w, r, steps)
			return
		}
	}

	pgName := strings.TrimSpace(r.FormValue("portgroup_name"))
	vlanID := formInt(r, "vlan_id")

	if !failed && r.FormValue("enable_phase2") == "on" {
		var entries []phases.VSwitchEntry
		for _, h := range hosts {
			entries = append(entries, phases.VSwitchEntry{BMHost: h, PGName: pgName, VlanID: vlanID})
		}
		targetVSwitch := strings.TrimSpace(r.FormValue("target_vswitch"))
		jobID, res, runErr := phases.RunPhase2(ctx, s.db, s.cfg, s.key, cred, phases.Phase2Input{TargetVSwitch: targetVSwitch, Entries: entries}, user.ID)
		if ok := runStep("Phase 2: vSwitch 포트그룹 생성", jobID, res, runErr); !ok && stopOnFail {
			s.renderPipelineResult(w, r, steps)
			return
		}
	}

	if !failed && r.FormValue("enable_phase3") == "on" {
		hostgroupMap := map[string]string{}
		if pgName != "" {
			for _, h := range hosts {
				hostgroupMap[h] = pgName
			}
		}
		in := phases.Phase3Input{
			Datacenter:   strings.TrimSpace(r.FormValue("datacenter")),
			Firmware:     strings.TrimSpace(r.FormValue("firmware")),
			VMCount:      formInt(r, "vm_count"),
			Hosts:        hosts,
			HostGroupMap: hostgroupMap,
			Specs: map[int]phases.VMSpec{
				1: {Cpu: formInt(r, "ev01_cpu"), Mem: formInt(r, "ev01_mem"), Disk: formInt(r, "ev01_disk"), Share: formInt(r, "ev01_share")},
				2: {Cpu: formInt(r, "ev02_cpu"), Mem: formInt(r, "ev02_mem"), Disk: formInt(r, "ev02_disk"), Share: formInt(r, "ev02_share")},
			},
		}
		jobID, res, runErr := phases.RunPhase3(ctx, s.db, s.cfg, s.key, cred, in, user.ID)
		if ok := runStep("Phase 3: VM 생성", jobID, res, runErr); !ok && stopOnFail {
			s.renderPipelineResult(w, r, steps)
			return
		}
	}

	if !failed && r.FormValue("enable_phase4") == "on" {
		in := phases.Phase4Input{
			Hosts:  hosts,
			Arg1:   strings.TrimSpace(r.FormValue("arg1")),
			ArgInt: formInt(r, "arg_int"),
			ArgStr: strings.TrimSpace(r.FormValue("arg_str")),
		}
		jobID, res, runErr := phases.RunPhase4(ctx, s.db, s.cfg, s.key, cred, in, user.ID)
		if ok := runStep("Phase 4: MAC/IP 추출", jobID, res, runErr); !ok && stopOnFail {
			s.renderPipelineResult(w, r, steps)
			return
		}
	}

	if !failed && r.FormValue("enable_phase5") == "on" {
		pairs := map[string]string{}
		for _, line := range splitLines(r.FormValue("affinity_pairs")) {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) == 2 && strings.TrimSpace(kv[0]) != "" && strings.TrimSpace(kv[1]) != "" {
				pairs[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
		jobID, res, runErr := phases.RunPhase5(ctx, s.db, s.cfg, s.key, cred, phases.Phase5Input{Hosts: hosts, AffinityPairs: pairs}, user.ID)
		if ok := runStep("Phase 5: CPU Affinity", jobID, res, runErr); !ok && stopOnFail {
			s.renderPipelineResult(w, r, steps)
			return
		}
	}

	if !failed && r.FormValue("enable_phase6") == "on" {
		in := phases.Phase6Input{
			Hosts:       hosts,
			EV01Cores:   formInt(r, "ev01_cores"),
			EV01Sockets: formInt(r, "ev01_sockets"),
			EV02Cores:   formInt(r, "ev02_cores"),
			EV02Sockets: formInt(r, "ev02_sockets"),
		}
		jobID, res, runErr := phases.RunPhase6(ctx, s.db, s.cfg, s.key, cred, in, user.ID)
		runStep("Phase 6: lpage/NUMA 튜닝", jobID, res, runErr)
	}

	s.renderPipelineResult(w, r, steps)
}

func (s *server) renderPipelineResult(w http.ResponseWriter, r *http.Request, steps []pipelineStep) {
	render(w, "pipeline_result", baseData(r, map[string]any{"Steps": steps}))
}

// --- shared helpers ---

func (s *server) finishJob(w http.ResponseWriter, r *http.Request, phaseName string, jobID int64, res *phases.Result, err error, userID int64, target string) {
	if err != nil {
		http.Error(w, "작업 실행 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	status := "success"
	auditResult := "success"
	switch {
	case res.DryRun:
		status = "dry_run"
		auditResult = "dry_run"
	case !res.Success:
		status = "failed"
		auditResult = "failed"
	}
	logAudit(s.db, userID, phaseName, target, auditResult)

	data := baseData(r, map[string]any{
		"Phase":  phaseName,
		"JobID":  jobID,
		"Status": status,
		"Lines":  res.Lines,
	})
	render(w, "job_result", data)
}

func (s *server) renderPhaseError(w http.ResponseWriter, r *http.Request, tmplName, msg string) {
	creds, _ := secrets.ListCredentials(s.db)
	render(w, tmplName, baseData(r, map[string]any{"Error": msg, "Credentials": creds}))
}

func baseData(r *http.Request, extra map[string]any) map[string]any {
	user, _ := auth.CurrentUser(r)
	data := map[string]any{"User": user, "Path": r.URL.Path}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

func render(w http.ResponseWriter, name string, data any) {
	t, ok := tmplSet[name]
	if !ok {
		http.Error(w, "알 수 없는 템플릿: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// fieldSplitter matches main_vm_create_v2.txt's loadHostgroupMap: fields may
// be separated by whitespace or commas, since users copy-paste from all sorts
// of sources ("host, pg, vlan" vs "host pg vlan").
var fieldSplitter = regexp.MustCompile(`[,\s]+`)

func splitFields(line string) []string {
	line = strings.Trim(line, " \t,")
	if line == "" {
		return nil
	}
	return fieldSplitter.Split(line, -1)
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func formInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.FormValue(key))
	return n
}

func logAudit(db *sql.DB, userID int64, action, target, result string) {
	_, _ = db.ExecContext(context.Background(), `INSERT INTO audit_log (user_id, action, target, result) VALUES (?, ?, ?, ?)`, userID, action, target, result)
}
