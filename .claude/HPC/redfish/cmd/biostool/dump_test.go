package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// dump.go 시험. 실제 BMC 는 쓰지 않고 internal/mockbmc(testdata 합성 트리)만 쓴다.

// fastDump 는 호스트당 요청 간격(기본 100ms)을 없애고 상한을 기본값으로 되돌립니다.
func fastDump(t *testing.T) {
	t.Helper()
	oldGap, oldMax := dumpClientGap, maxDumpResources
	dumpClientGap = -1
	t.Cleanup(func() { dumpClientGap, maxDumpResources = oldGap, oldMax })
}

func dumpRC(t *testing.T, conc int, urls ...string) *runContext {
	t.Helper()
	var in []string
	for _, u := range urls {
		in = append(in, strings.TrimPrefix(u, "https://"))
	}
	ts, err := resolveTargets(in, filepath.Join(t.TempDir(), "hosts-없어도-IP-만이면-읽지-않음"))
	if err != nil {
		t.Fatal(err)
	}
	return &runContext{
		Conf:     &Config{User: testUser, Concurrency: conc, Timeout: 5 * time.Second, Insecure: true, Retries: 0, DumpDir: "dumps"},
		Targets:  ts,
		Password: testPass,
	}
}

func summaryText(rs []*dumpResult, compact bool, out string) string {
	var b bytes.Buffer
	writeDumpSummary(&b, rs, dumpOpts{Out: out, Compact: compact})
	return b.String()
}

// listFiles 는 root 아래 일반 파일의 상대 경로(슬래시, 소문자) 집합입니다.
func listFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			m[strings.ToLower(filepath.ToSlash(rel))] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// (a)(b)(d)(h) 6개 트리를 각각 덤프한다.
func TestDumpAllTrees(t *testing.T) {
	fastDump(t)
	for _, name := range mockTrees {
		name := name
		t.Run(name, func(t *testing.T) {
			s := startMockTree(t, name, nil)
			out := t.TempDir()
			rc := dumpRC(t, 1, s.URL())
			rs, err := runDump(rc, dumpOpts{Out: out})
			if err != nil {
				t.Fatalf("runDump: %v", err)
			}
			r := rs[0]
			if r.Status != "OK" || r.Info == nil || r.Sum == nil || len(r.Notes) != 0 {
				t.Fatalf("Status=%s Notes=%v Info=%+v", r.Status, r.Notes, r.Info)
			}
			if r.RelDir == "" {
				t.Fatal("저장 경로가 비어 있음")
			}

			// (a) 저장 파일 ⊆ 트리 파일, 내용 동일, 핵심 리소스 포함, LogServices·SessionService 없음
			hostDir := filepath.Join(out, filepath.FromSlash(r.RelDir))
			saved := listFiles(t, hostDir)
			tree := listFiles(t, filepath.Join("../../testdata", name))
			var hasSys, hasBios, hasSettings, hasRegistry bool
			for rel, p := range saved {
				if rel == "dump_meta.json" {
					continue
				}
				tp, ok := tree[rel]
				if !ok {
					t.Errorf("트리에 없는 파일이 저장됨: %s", rel)
					continue
				}
				a, _ := os.ReadFile(p)
				b, _ := os.ReadFile(tp)
				if !bytes.Equal(a, b) {
					t.Errorf("%s: 저장 내용이 BMC 응답과 다름", rel)
				}
				if strings.Contains(rel, "logservices") || strings.Contains(rel, "sessionservice") || strings.Contains(rel, "/entries") {
					t.Errorf("저장되면 안 되는 경로: %s", rel)
				}
				base := rel[strings.LastIndex(rel, "/")+1:]
				switch {
				case base == "bios.json":
					hasBios = true
				case base == "settings.json" || base == "pending.json":
					hasSettings = true
				case strings.HasPrefix(rel, "redfish/v1/systems/") && strings.Count(rel, "/") == 3:
					hasSys = true
				}
			}
			_, hasRegistry = saved[strings.ToLower(resourceFile(r.Info.RegistryPath))]
			if !hasSys || !hasBios || !hasRegistry {
				t.Errorf("핵심 리소스 누락: system=%t bios=%t registry=%t (%v)", hasSys, hasBios, hasRegistry, keys(saved))
			}
			if name == "supermicro-x12" {
				if hasSettings || r.Info.SettingsPath != "" {
					t.Errorf("supermicro 는 Settings 가 없어야 함: %q", r.Info.SettingsPath)
				}
			} else if !hasSettings {
				t.Errorf("Settings/Pending 누락")
			}
			if r.Resources != len(saved)-1 {
				t.Errorf("Resources=%d, 저장 파일 %d", r.Resources, len(saved)-1)
			}

			// (b) mock 집계: 로그·쓰기 0, 세션 생성 1·삭제 1, POST 는 세션 생성뿐
			if s.LogHits() != 0 || s.Writes() != 0 {
				t.Errorf("LogHits=%d Writes=%d", s.LogHits(), s.Writes())
			}
			if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 || ss.LoginFailed != 0 {
				t.Errorf("세션 %+v", ss)
			}
			if p := s.Count("POST", "/"); p != 1 {
				t.Errorf("POST %d회, 기대 1 (세션 생성)", p)
			}
			if g := s.Count("GET", "/"); g != r.Stats.Gets || g > maxDumpResources {
				t.Errorf("mock GET %d, client GET %d, 상한 %d", g, r.Stats.Gets, maxDumpResources)
			}
			if r.Stats.AuthMode != "session" || r.Stats.Sessions != 1 {
				t.Errorf("Stats %+v", r.Stats)
			}
			if r.Skipped < 1 {
				t.Errorf("System/Manager 의 LogServices 링크가 skipped 로 집계돼야 함: %d", r.Skipped)
			}

			// (h) 권한: 파일 0600, 디렉터리 0700
			if runtime.GOOS != "windows" {
				filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
					if err != nil || p == out {
						return err
					}
					fi, _ := d.Info()
					want := fs.FileMode(0o600)
					if d.IsDir() {
						want = 0o700
					}
					if fi.Mode().Perm() != want {
						t.Errorf("%s 권한 %v, 기대 %v", p, fi.Mode().Perm(), want)
					}
					return nil
				})
			}

			// dump_meta.json
			var meta dumpMeta
			mb, err := os.ReadFile(filepath.Join(hostDir, "dump_meta.json"))
			if err != nil || json.Unmarshal(mb, &meta) != nil {
				t.Fatalf("dump_meta.json: %v", err)
			}
			if meta.Auth != "session" || meta.Calls.Get != r.Stats.Gets || meta.Calls.Delete != 1 || meta.Calls.Post != 1 ||
				meta.Skipped != r.Skipped || meta.ToolVersion == "" || meta.Time == "" || meta.Vendor != r.Info.Vendor || meta.Status != "OK" {
				t.Errorf("meta = %+v", meta)
			}
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDumpFolderLayoutAndSanitize(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "hpe-dl360gen11", nil)
	out := t.TempDir()
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: out})
	// 공백·괄호·슬래시는 _ 로, 연속 _ 는 하나로: vendor/model/biosver/host
	want := "HPE/ProLiant_DL360_Gen11/U54_v1.40_01_15_2025/" + dirPart(rs[0].Target.Hostname)
	if rs[0].RelDir != want {
		t.Errorf("RelDir = %q, 기대 %q", rs[0].RelDir, want)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(want), "redfish", "v1.json")); err != nil {
		t.Errorf("루트는 redfish/v1.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(want), "redfish", "v1", "systems", "1.json")); err != nil {
		t.Errorf("BMC 가 알려 준 대소문자 그대로 저장: %v", err)
	}
}

func TestSanitizeNames(t *testing.T) {
	cases := map[string]string{
		"PowerEdge R660":    "PowerEdge_R660",
		"a/b\\c":            "a_b_c",
		"..":                "unknown",
		"../../etc":         "etc",
		"1..2":              "1_2",
		"  ":                "unknown",
		"":                  "unknown",
		".hidden":           "hidden",
		"U54 v1.40 (01/15)": "U54_v1.40_01_15",
		"127.0.0.1:8443":    "127.0.0.1_8443",
		"[::1]:8443":        "1_8443",
		"한글 모델":             "unknown",
		"model\x00\n\tx":    "model_x",
	}
	for in, want := range cases {
		if got := dirPart(in); got != want {
			t.Errorf("dirPart(%q) = %q, 기대 %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"System.Embedded.1": "System.Embedded.1", "..": "_", ".": "_", "": "_", "a b": "a_b", "X.v1_0_0.json": "X.v1_0_0.json"} {
		if got := safeSeg(in); got != want {
			t.Errorf("safeSeg(%q) = %q, 기대 %q", in, got, want)
		}
	}
	if got := resourceFile("/redfish/v1"); got != "redfish/v1.json" {
		t.Errorf("루트 = %q", got)
	}
	if got := resourceFile("/redfish/v1/Registries/X.v1_0_0.json"); got != "redfish/v1/Registries/X.v1_0_0.json.json" {
		t.Errorf("레지스트리 = %q", got)
	}
	if got := resourceFile("/redfish/v1/a b/../x"); strings.Contains(got, "..") {
		t.Errorf("상위 경로가 남음: %q", got)
	}
}

// (c) 요약의 표준 4항목 후보
func TestDumpSummaryCandidates(t *testing.T) {
	fastDump(t)
	type tc struct {
		tree  string
		must  []string
		regex []string
	}
	cases := []tc{
		{"dell-r660", []string{
			"== ", "Dell PowerEdge R660 1.8.2 auth=session",
			"settings=/redfish/v1/Systems/System.Embedded.1/Bios/Settings (SettingsObject)",
			"SysProfile = PerfOptimized [허용값: PerfPerWattOptimizedDapc | PerfPerWattOptimizedOs | PerfOptimized | Custom]",
			"LogicalProc = Enabled [허용값: Enabled | Disabled]",
			"LlcPrefetch = Enabled", "SubNumaCluster = Disabled", "pending=없음 (settings_diff=0 jobs=0)",
			"registry=BiosAttributeRegistry.v1_0_0",
		}, nil},
		{"hpe-dl360gen11", []string{"HPE ProLiant DL360 Gen11", "WorkloadProfile = GeneralPowerEfficientCompute [허용값: ", "ProcHyperthreading = Enabled [허용값: ", "LlcPrefetch = Enabled", "SubNumaClustering = Disabled"}, nil},
		{"hpe-dl360gen10", []string{"WorkloadProfile = ", "ProcHyperthreading = ", "SubNumaClustering = "}, []string{`llc_prefetch\s+후보 없음`}},
		{"lenovo-sr650v3", []string{"Lenovo ThinkSystem SR650 V3", "Processors_HyperThreading = Enable", "Processors_LLCPrefetch = ", "Processors_SNC = ", "(SettingsObject)"}, nil},
		{"cisco-c220m7", []string{"Cisco UCSC-C220-M7S", "IntelHyperThread = enabled", "LLCPrefetch = ", "SNC = "}, nil},
		{"supermicro-x12", []string{"Supermicro X12DPi-N6", "Hyper-Threading[ALL] = ", "LLCPrefetch = ", "settings=없음", "pending=확인불가"}, nil},
	}
	for _, c := range cases {
		c := c
		t.Run(c.tree, func(t *testing.T) {
			s := startMockTree(t, c.tree, nil)
			rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()})
			text := summaryText(rs, false, "dumps")
			for _, m := range c.must {
				if !strings.Contains(text, m) {
					t.Errorf("요약에 %q 없음:\n%s", m, text)
				}
			}
			for _, re := range c.regex {
				if !regexp.MustCompile(re).MatchString(text) {
					t.Errorf("요약이 %s 와 맞지 않음:\n%s", re, text)
				}
			}
			// 사람이 옮겨 적기 쉽게 짧아야 한다
			if n := strings.Count(text, "\n"); n > 14 {
				t.Errorf("요약이 %d줄 (너무 김):\n%s", n, text)
			}
		})
	}
}

func TestDumpSummaryCompact(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()})
	text := summaryText(rs, true, "dumps")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 2 { // 호스트 1줄 + 합계 1줄
		t.Fatalf("compact 는 호스트당 1줄이어야 함:\n%s", text)
	}
	for _, m := range []string{"Dell PowerEdge R660 1.8.2 auth=session", "system_profile=SysProfile:PerfOptimized", "hyper_threading=LogicalProc:Enabled", "llc_prefetch=LlcPrefetch:Enabled", "sub_numa_cluster=SubNumaCluster:Disabled", "attrs=19"} {
		if !strings.Contains(lines[0], m) {
			t.Errorf("compact 줄에 %q 없음: %s", m, lines[0])
		}
	}
	if strings.Contains(text, "허용값") {
		t.Errorf("compact 에는 허용값 목록이 없어야 함")
	}
}

// 기존 Pending/Job 감지 지표
func TestDumpPendingIndicators(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	// Settings 에 현재와 다른 속성 2개 + 현재와 같은 속성 1개, Jobs 멤버 2개
	if err := s.Modify("/redfish/v1/Systems/System.Embedded.1/Bios/Settings", func(m map[string]interface{}) {
		m["Attributes"] = map[string]interface{}{"LogicalProc": "Disabled", "NewAttr": "x", "SysProfile": "PerfOptimized"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Modify("/redfish/v1/Managers/iDRAC.Embedded.1/Jobs", func(m map[string]interface{}) {
		m["Members"] = []interface{}{map[string]interface{}{"@odata.id": "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs/JID_1"}, map[string]interface{}{"@odata.id": "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs/JID_2"}}
	}); err != nil {
		t.Fatal(err)
	}
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()})
	text := summaryText(rs, false, "dumps")
	if !strings.Contains(text, "pending=있음 (settings_diff=2 jobs=2)") {
		t.Errorf("Pending 지표:\n%s", text)
	}
	// Job 멤버 자체는 따라가지 않는다 (목록만)
	if n := s.Count("GET", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs/"); n != 0 {
		t.Errorf("Job 멤버를 조회함: %d회", n)
	}
}

// Manager 문서에 Jobs 링크가 없는 Dell 은 <Manager>/Jobs 를 추정해 읽는다.
func TestDumpDellJobsFallback(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	if err := s.Modify("/redfish/v1/Managers/iDRAC.Embedded.1", func(m map[string]interface{}) { delete(m, "Oem") }); err != nil {
		t.Fatal(err)
	}
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()})
	if !strings.Contains(summaryText(rs, false, "d"), "jobs=0") || s.Count("GET", "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs") != 1 {
		t.Errorf("Dell Jobs 추정 조회 실패: %s", summaryText(rs, false, "d"))
	}
}

// System 이 여러 개면 모두 목록화·덤프하고 첫 번째를 요약에 쓴다.
func TestDumpMultipleSystems(t *testing.T) {
	fastDump(t)
	tree := t.TempDir()
	copyTree(t, "../../testdata/dell-r660", tree)
	sys := filepath.Join(tree, "redfish/v1/Systems")
	for _, f := range []string{"System.Embedded.1.json", "System.Embedded.1/Bios.json", "System.Embedded.1/Bios/Settings.json"} {
		b, err := os.ReadFile(filepath.Join(sys, f))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(sys, strings.ReplaceAll(f, "System.Embedded.1", "System.Embedded.2"))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, []byte(strings.ReplaceAll(string(b), "System.Embedded.1", "System.Embedded.2")), 0o644)
	}
	col := `{"@odata.id":"/redfish/v1/Systems","Members":[{"@odata.id":"/redfish/v1/Systems/System.Embedded.1"},{"@odata.id":"/redfish/v1/Systems/System.Embedded.2"}],"Oem":{"MockData":true}}`
	os.WriteFile(filepath.Join(tree, "redfish/v1/Systems.json"), []byte(col), 0o644)
	srv, err := mockbmc.New(tree, mockbmc.Options{User: testUser, Pass: testPass})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	defer srv.Close()

	out := t.TempDir()
	rs, _ := runDump(dumpRC(t, 1, srv.URL()), dumpOpts{Out: out})
	r := rs[0]
	if r.Status != "OK" || len(r.Info.Systems) != 2 || r.Info.SystemPath != "/redfish/v1/Systems/System.Embedded.1" {
		t.Fatalf("Status=%s notes=%v info=%+v", r.Status, r.Notes, r.Info)
	}
	saved := listFiles(t, filepath.Join(out, filepath.FromSlash(r.RelDir)))
	for _, f := range []string{"redfish/v1/systems/system.embedded.2.json", "redfish/v1/systems/system.embedded.2/bios.json", "redfish/v1/systems/system.embedded.2/bios/settings.json"} {
		if _, ok := saved[f]; !ok {
			t.Errorf("두 번째 System 파일 누락: %s", f)
		}
	}
	if !strings.Contains(summaryText(rs, false, "d"), "systems=2(첫 번째 사용)") {
		t.Errorf("systems=2 표기 없음:\n%s", summaryText(rs, false, "d"))
	}
	if srv.LogHits() != 0 || srv.Writes() != 0 {
		t.Errorf("LogHits=%d Writes=%d", srv.LogHits(), srv.Writes())
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// (e) 20대 동시 mock 병렬 덤프 (-race)
func TestDumpParallel20(t *testing.T) {
	fastDump(t)
	const n = 20
	var urls []string
	var servers []*mockbmc.Server
	for i := 0; i < n; i++ {
		s := startMockTree(t, mockTrees[i%len(mockTrees)], nil)
		servers = append(servers, s)
		urls = append(urls, s.URL())
	}
	out := t.TempDir()
	rs, err := runDump(dumpRC(t, n, urls...), dumpOpts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rs {
		if r.Status != "OK" {
			t.Errorf("host %d: %s %v", i, r.Status, r.Notes)
		}
		s := servers[i]
		if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 || s.LogHits() != 0 || s.Writes() != 0 {
			t.Errorf("host %d: 세션 %+v LogHits=%d Writes=%d", i, ss, s.LogHits(), s.Writes())
		}
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(idx), "\n"), "\n")
	if len(lines) != n+1 {
		t.Fatalf("index.tsv %d줄, 기대 %d (머리글 + %d)", len(lines), n+1, n)
	}
	if lines[0] != "host\tip\tvendor\tmodel\tbiosver\tpath\tstatus" {
		t.Errorf("머리글: %q", lines[0])
	}
	for _, l := range lines[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 7 || f[6] != "OK" || f[5] == "-" {
			t.Errorf("index 행: %q", l)
		}
	}
	// 호스트 폴더도 20개 (같은 모델이어도 포트가 달라 구분됨)
	cnt := 0
	for rel := range listFiles(t, out) {
		if strings.HasSuffix(rel, "dump_meta.json") {
			cnt++
		}
	}
	if cnt != n {
		t.Errorf("dump_meta.json %d개, 기대 %d", cnt, n)
	}
}

// (f) 일부 호스트가 401 / 연결 불가 / 503 이어도 나머지는 끝까지 처리되고 상태 코드가 표기된다.
func TestDumpPartialFailures(t *testing.T) {
	fastDump(t)
	good := startMockTree(t, "dell-r660", nil)
	badPass := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" })
	down := startMockTree(t, "lenovo-sr650v3", nil)
	downURL := down.URL()
	down.Close() // 연결 거부
	sick := startMockTree(t, "cisco-c220m7", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"/redfish/v1/Systems": {Status: 503}}
	})
	good2 := startMockTree(t, "supermicro-x12", nil)

	out := t.TempDir()
	rc := dumpRC(t, 5, good.URL(), badPass.URL(), downURL, sick.URL(), good2.URL())
	rs, err := runDump(rc, dumpOpts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rs {
		got = append(got, fmt.Sprintf("%s/%d", r.Status, r.HTTP))
	}
	want := []string{"OK/0", "AUTH_FAIL/401", "UNREACHABLE/0", "BMC_ERROR/503", "OK/0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("상태 %v, 기대 %v", got, want)
	}
	// 401 은 재시도하지 않는다: 로그인 시도 1번뿐, 세션 없음
	if ss := badPass.Sessions(); ss.LoginFailed != 1 || ss.Created != 0 || badPass.Count("", "/") != 1 {
		t.Errorf("401 호스트: %+v 요청 %d건", ss, badPass.Count("", "/"))
	}
	// 503 호스트도 만든 세션은 지운다
	if ss := sick.Sessions(); ss.Created != 1 || ss.Deleted != 1 {
		t.Errorf("503 호스트 세션: %+v", ss)
	}

	text := summaryText(rs, false, out)
	for _, m := range []string{"AUTH_FAIL (HTTP 401)", "UNREACHABLE", "BMC_ERROR (HTTP 503)", "OK 2 / PARTIAL 0 / 실패 3"} {
		if !strings.Contains(text, m) {
			t.Errorf("요약에 %q 없음:\n%s", m, text)
		}
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.tsv"))
	lines := strings.Split(strings.TrimRight(string(idx), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("index.tsv %d줄, 기대 6:\n%s", len(lines), idx)
	}
	for i, st := range []string{"OK", "AUTH_FAIL(HTTP 401)", "UNREACHABLE", "BMC_ERROR(HTTP 503)", "OK"} {
		f := strings.Split(lines[i+1], "\t")
		if f[6] != st {
			t.Errorf("index %d 상태 %q, 기대 %q", i, f[6], st)
		}
		if (st == "OK") == (f[5] == "-") {
			t.Errorf("index %d 경로 %q (성공만 경로가 있어야 함)", i, f[5])
		}
	}
	// 실패 호스트는 폴더를 만들지 않는다 (성공 2대의 meta 만)
	cnt := 0
	for rel := range listFiles(t, out) {
		if strings.HasSuffix(rel, "dump_meta.json") {
			cnt++
		}
	}
	if cnt != 2 {
		t.Errorf("dump_meta.json %d개, 기대 2", cnt)
	}
}

// 부분 실패(레지스트리 404): PARTIAL 로 표시하되 나머지는 저장한다.
func TestDumpPartialNote(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	s.Remove("/redfish/v1/Managers/iDRAC.Embedded.1")
	out := t.TempDir()
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: out})
	r := rs[0]
	if r.Status != "PARTIAL" || len(r.Notes) == 0 || r.Sum == nil || r.Resources == 0 {
		t.Fatalf("Status=%s notes=%v", r.Status, r.Notes)
	}
	text := summaryText(rs, false, out)
	if !strings.Contains(text, "PARTIAL") || !strings.Contains(text, "note(") || !strings.Contains(text, "HTTP 404") {
		t.Errorf("PARTIAL 표기:\n%s", text)
	}
	if !strings.Contains(text, "OK 0 / PARTIAL 1 / 실패 0") {
		t.Errorf("합계:\n%s", text)
	}
}

// 호스트당 리소스 상한
func TestDumpResourceLimit(t *testing.T) {
	fastDump(t)
	maxDumpResources = 5
	s := startMockTree(t, "dell-r660", nil)
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: t.TempDir()})
	r := rs[0]
	if g := s.Count("GET", "/"); g > 5 {
		t.Errorf("GET %d회, 상한 5", g)
	}
	if r.Status != "PARTIAL" || !strings.Contains(strings.Join(r.Notes, ";"), "상한") {
		t.Errorf("Status=%s notes=%v", r.Status, r.Notes)
	}
	if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 {
		t.Errorf("상한에 걸려도 세션은 지운다: %+v", ss)
	}
}

// 세션 서비스가 없는 BMC: Basic 인증 폴백
func TestDumpBasicAuthFallback(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.NoSessions = true })
	out := t.TempDir()
	rs, _ := runDump(dumpRC(t, 1, s.URL()), dumpOpts{Out: out})
	if rs[0].Status != "OK" || rs[0].Stats.AuthMode != "basic" {
		t.Fatalf("Status=%s auth=%q", rs[0].Status, rs[0].Stats.AuthMode)
	}
	if !strings.Contains(summaryText(rs, false, out), "auth=basic") {
		t.Errorf("요약에 auth=basic 없음")
	}
	var meta dumpMeta
	b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(rs[0].RelDir), "dump_meta.json"))
	if json.Unmarshal(b, &meta) != nil || meta.Auth != "basic" {
		t.Errorf("meta.auth = %q", meta.Auth)
	}
}

// 재덤프는 덮어쓰고(옛 파일 제거), index.tsv 는 이어 쓴다.
func TestDumpRedumpOverwrites(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	out := t.TempDir()
	rc := dumpRC(t, 1, s.URL())
	rs, _ := runDump(rc, dumpOpts{Out: out})
	stale := filepath.Join(out, filepath.FromSlash(rs[0].RelDir), "redfish", "stale.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runDump(rc, dumpOpts{Out: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("재덤프 후 옛 파일이 남음")
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.tsv"))
	if n := strings.Count(string(idx), "\n"); n != 3 {
		t.Errorf("index.tsv %d줄, 기대 3 (머리글 + 2회 실행)", n)
	}
	if n := strings.Count(string(idx), "host\tip\t"); n != 1 {
		t.Errorf("머리글이 %d번 기록됨", n)
	}
}

// (g) 요약·meta·index·저장 JSON 어디에도 비밀번호/토큰/시리얼 등이 새지 않는다 (요약·meta·index 기준).
func TestDumpNoSecretsInOutputs(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	bad := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.Pass = "다른" })
	out := t.TempDir()
	rc := dumpRC(t, 2, s.URL(), bad.URL())
	rs, _ := runDump(rc, dumpOpts{Out: out})
	var all strings.Builder
	all.WriteString(summaryText(rs, false, out))
	all.WriteString(summaryText(rs, true, out))
	for rel, p := range listFiles(t, out) {
		if rel == "index.tsv" || strings.HasSuffix(rel, "dump_meta.json") {
			b, _ := os.ReadFile(p)
			all.Write(b)
		}
	}
	text := all.String()
	for _, secret := range []string{testPass, "다른-비밀번호", "X-Auth-Token", "Basic ", "Password", "MOCK-SN-DELL-0001", "MOCKSV1", "MOCK-ASSET-0001", "00000000-0000-4000-8000"} {
		if strings.Contains(text, secret) {
			t.Errorf("출력에 %q 가 들어 있음", secret)
		}
	}
}

// 대상 해석 실패 (NO_HOSTS_ENTRY) 도 한 줄로 표시되고 index 에 기록된다.
func TestDumpNoHostsEntry(t *testing.T) {
	fastDump(t)
	hosts := writeHosts(t, "192.0.2.10 other-m\n")
	ts, err := resolveTargets([]string{"ghost"}, hosts)
	if err != nil {
		t.Fatal(err)
	}
	rc := &runContext{Conf: &Config{User: testUser, Concurrency: 1, Timeout: time.Second}, Targets: ts, Password: testPass}
	out := t.TempDir()
	rs, _ := runDump(rc, dumpOpts{Out: out})
	text := summaryText(rs, false, out)
	if !strings.Contains(text, "!! ghost NO_HOSTS_ENTRY") || !strings.Contains(text, "실패 1") {
		t.Errorf("요약:\n%s", text)
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.tsv"))
	if !strings.Contains(string(idx), "ghost\t-\t-\t-\t-\t-\tNO_HOSTS_ENTRY") {
		t.Errorf("index:\n%s", idx)
	}
}

// 상한·정책 시험용: 허용목록 밖 링크는 따라가지 않고 skipped 로 센다.
func TestDumpSessionSkipsDeniedPaths(t *testing.T) {
	fastDump(t)
	s := startMockTree(t, "dell-r660", nil)
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: testUser, Pass: testPass, Insecure: true, Gap: -1, Mode: ModeReadOnly})
	defer c.Close()
	ds := newDumpSession(c)
	for _, p := range []string{
		"/redfish/v1/Systems/System.Embedded.1/LogServices",
		"/redfish/v1/Systems/System.Embedded.1/LogServices/Sel/Entries",
		"/redfish/v1/Systems/System.Embedded.1/Actions/ComputerSystem.Reset",
	} {
		if _, err := ds.fetch(p, "x"); err == nil {
			t.Errorf("%s 가 허용됨", p)
		}
	}
	// 같은 경로를 대소문자만 바꿔 다시 요청해도 한 번으로 센다
	ds.fetch("/redfish/v1/systems/system.embedded.1/logservices/", "x")
	if len(ds.skipped) != 3 || len(ds.notes) != 0 || ds.calls != 0 {
		t.Errorf("skipped=%d notes=%v calls=%d", len(ds.skipped), ds.notes, ds.calls)
	}
	if s.Count("", "/") != 0 {
		t.Errorf("거부된 호출이 mock 에 도달함")
	}
}

func TestCmdDumpRejectsFromDump(t *testing.T) {
	if err := cmdDump([]string{"-from-dump", "x", "-targets", "127.0.0.1:1"}); err == nil || !strings.Contains(err.Error(), "-from-dump") {
		t.Errorf("err=%v", err)
	}
}
