package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sq(v, typ string, req bool) surveyQ {
	return surveyQ{Variable: v, QuestionName: v + " 질문", Type: typ, Required: req}
}

func stressSpec() []surveyQ {
	return []surveyQ{sq("end_year", "integer", true), sq("end_month", "integer", true), sq("end_day", "text", true),
		sq("end_hour", "float", true), sq("end_min", "text", true), sq("target_hosts", "textarea", true), sq("mode", "text", false)}
}

func msgs(fs []finding, kind int) string {
	var m []string
	for _, f := range fs {
		if f.kind == kind {
			m = append(m, f.msg)
		}
	}
	return strings.Join(m, "\n")
}

func TestCheckKeysCPU(t *testing.T) {
	spec := []surveyQ{sq("target_hosts", "textarea", true), sq("intensity", "integer", false)}
	// 정상
	fs := checkKeys(stageCPU, TemplateConf{HostnameKey: "target_hosts"}, nil, true, spec)
	if len(fs) != 1 || fs[0].kind != fOK || !strings.Contains(fs[0].msg, "type textarea") {
		t.Errorf("정상: %+v", fs)
	}
	// hostname key 가 survey 에 없음 + 그 때문에 target_hosts 는 필수인데 conf 에 없음
	fs = checkKeys(stageCPU, TemplateConf{HostnameKey: "hosts"}, nil, true, spec)
	if !strings.Contains(msgs(fs, fBad), "cpu_hostname_key = hosts 가 survey 에 없습니다") || !strings.Contains(msgs(fs, fWarn), "필수 survey 항목이 conf 에 없음: target_hosts") {
		t.Errorf("없는 키: %+v", fs)
	}
	// hostname key 비움: 참고만 + 필수 항목은 경고
	fs = checkKeys(stageCPU, TemplateConf{}, nil, true, spec)
	if !strings.Contains(msgs(fs, fInfo), "인벤토리 전체 대상") || !strings.Contains(msgs(fs, fWarn), "target_hosts") {
		t.Errorf("비움: %+v", fs)
	}
	// extra_vars 로 채우면 경고 없음
	fs = checkKeys(stageCPU, TemplateConf{ExtraVars: "target_hosts=x"}, nil, true, spec)
	if msgs(fs, fWarn) != "" {
		t.Errorf("extra_vars 로 채움: %+v", fs)
	}
	// 기본값 있는 필수 항목은 경고에 기본값을 보임
	spec[1].Required, spec[1].Default = true, float64(80)
	fs = checkKeys(stageCPU, TemplateConf{HostnameKey: "target_hosts"}, nil, true, spec)
	if !strings.Contains(msgs(fs, fWarn), "intensity (survey 기본값 80)") {
		t.Errorf("기본값: %+v", fs)
	}
	// survey 꺼짐
	fs = checkKeys(stageCPU, TemplateConf{HostnameKey: "target_hosts"}, nil, false, spec)
	if !strings.Contains(msgs(fs, fWarn), "survey 가 꺼져 있습니다") {
		t.Errorf("survey 꺼짐: %+v", fs)
	}
}

func TestCheckKeysStress(t *testing.T) {
	spec := stressSpec()
	keys := []string{"end_year", "end_month", "end_day", "end_hour", "end_min"}
	fs := checkKeys(stageStress, TemplateConf{HostnameKey: "target_hosts"}, keys, true, spec)
	if msgs(fs, fBad) != "" || msgs(fs, fWarn) != "" {
		t.Fatalf("정상인데 문제: %+v", fs)
	}
	ok := msgs(fs, fOK)
	for _, w := range []string{"end_year: survey type integer → 숫자로 보냄", "end_day: survey type text → 입력한 글자 그대로 보냄", "end_hour: survey type float → 숫자(실수)로 보냄"} {
		if !strings.Contains(ok, w) {
			t.Errorf("%q 없음: %s", w, ok)
		}
	}
	// 종료 시각 키 하나가 survey 에 없음
	keys[4] = "end_minute"
	fs = checkKeys(stageStress, TemplateConf{HostnameKey: "target_hosts"}, keys, true, spec)
	if !strings.Contains(msgs(fs, fBad), "stress_end_keys = end_minute 가 survey 에 없습니다") || !strings.Contains(msgs(fs, fWarn), "end_min") {
		t.Errorf("없는 end 키: %+v", fs)
	}
}

func TestSuggestLines(t *testing.T) {
	got := suggestLines(stageStress, "stress-ng", stressSpec())
	want := []string{"stress_template = stress-ng", "stress_hostname_key = target_hosts", "stress_end_keys = end_year,end_month,end_day,end_hour,end_min"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stress 제안 = %q", got)
	}
	got = suggestLines(stageCPU, "cpu-test", []surveyQ{sq("a_host", "text", true), sq("hostlist", "text", false)})
	if got[0] != "cpu_template = cpu-test" || !strings.HasPrefix(got[1], "cpu_hostname_key = a_host") || !strings.Contains(got[1], "후보: a_host, hostlist") {
		t.Errorf("cpu 제안 = %q", got)
	}
	got = suggestLines(stageCPU, "cpu-test", []surveyQ{sq("x", "text", false)})
	if !strings.HasPrefix(got[1], "# cpu_hostname_key =") {
		t.Errorf("후보 없음 = %q", got)
	}
}

func TestEndCandidates(t *testing.T) {
	// 순서가 섞여 있어도 연월일시분 순서로 정렬
	spec := []surveyQ{sq("end_min", "text", true), sq("end_year", "text", true), sq("end_day", "text", true), sq("end_month", "text", true), sq("end_hour", "text", true), sq("endx", "text", false)}
	if got := endCandidates(spec); !reflect.DeepEqual(got, []string{"end_year", "end_month", "end_day", "end_hour", "end_min"}) {
		t.Errorf("정렬 = %v", got)
	}
	// 5개가 아니면 survey 순서 그대로 + 제안 줄에 개수 안내
	spec = []surveyQ{sq("end_a", "text", true), sq("end_b", "text", true)}
	if got := endCandidates(spec); !reflect.DeepEqual(got, []string{"end_a", "end_b"}) {
		t.Errorf("2개 = %v", got)
	}
	if l := suggestLines(stageStress, "s", spec); !strings.Contains(l[len(l)-1], "5개여야 함 (end_ 변수 2개 검출)") {
		t.Errorf("개수 안내 = %q", l)
	}
	if l := suggestLines(stageStress, "s", nil); !strings.HasPrefix(l[len(l)-1], "# stress_end_keys =") {
		t.Errorf("없음 = %q", l)
	}
}

func TestSurveyRows(t *testing.T) {
	var q surveyQ
	json.Unmarshal([]byte(`{"variable":"mode","question_name":"모드","type":"multiplechoice","required":false,"default":"all","choices":"all\ncpu\nvm","min":null,"max":null}`), &q)
	var n surveyQ
	json.Unmarshal([]byte(`{"variable":"intensity","type":"integer","required":true,"default":80,"choices":"","min":1,"max":100}`), &n)
	rows := surveyRows([]surveyQ{q, n})
	if !reflect.DeepEqual(rows[0], []string{"mode", "모드", "multiplechoice", "false", "all", "all,cpu,vm", "-"}) {
		t.Errorf("row0 = %q", rows[0])
	}
	if !reflect.DeepEqual(rows[1], []string{"intensity", "-", "integer", "true", "80", "-", "1/100"}) {
		t.Errorf("row1 = %q", rows[1])
	}
	if got := choicesText([]interface{}{"a", "b"}); got != "a,b" {
		t.Errorf("배열 choices = %q", got)
	}
}

// inspSrv 는 -inspect 시험용 가짜 AWX 다. GET 이 아닌 요청은 모두 기록한다.
type inspSrv struct {
	tmplInv   int
	invs      []invInfo
	stress    []surveyQ
	cpu       []surveyQ
	srcInv    int // 소스 7 이 속한 인벤토리
	writes    []string
	surveyOff bool
}

func (s *inspSrv) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writes = append(s.writes, r.Method+" "+r.URL.Path)
		w.WriteHeader(500)
		return
	}
	enc := json.NewEncoder(w)
	invName := func(id int) string {
		for _, v := range s.invs {
			if v.ID == id {
				return v.Name
			}
		}
		return ""
	}
	tmpl := func(id int, name string) map[string]interface{} {
		return map[string]interface{}{"id": id, "name": name, "inventory": s.tmplInv, "ask_inventory_on_launch": true, "survey_enabled": !s.surveyOff,
			"extra_vars": "---\nfoo: bar\n", "summary_fields": map[string]interface{}{"inventory": map[string]interface{}{"id": s.tmplInv, "name": invName(s.tmplInv)}}}
	}
	src := map[string]interface{}{"id": 7, "name": "src1", "inventory": s.srcInv, "source": "scm", "source_path": "x.yml", "overwrite": true, "overwrite_vars": false}
	switch r.URL.Path {
	case "/api/v2/inventories/":
		enc.Encode(map[string]interface{}{"results": s.invs})
	case "/api/v2/inventories/3/inventory_sources/":
		enc.Encode(map[string]interface{}{"results": []interface{}{src}})
	case "/api/v2/inventories/8/inventory_sources/":
		enc.Encode(map[string]interface{}{"results": []interface{}{}})
	case "/api/v2/inventory_sources/7/":
		enc.Encode(src)
	case "/api/v2/job_templates/":
		enc.Encode(map[string]interface{}{"results": []interface{}{tmpl(5, "stress-ng"), tmpl(6, "cpu-test")}})
	case "/api/v2/job_templates/5/":
		enc.Encode(tmpl(5, "stress-ng"))
	case "/api/v2/job_templates/6/":
		enc.Encode(tmpl(6, "cpu-test"))
	case "/api/v2/job_templates/5/survey_spec/":
		enc.Encode(map[string]interface{}{"spec": s.stress})
	case "/api/v2/job_templates/6/survey_spec/":
		enc.Encode(map[string]interface{}{"spec": s.cpu})
	default:
		w.WriteHeader(404)
	}
}

func newInspect(t *testing.T) (*inspSrv, *Config, *awxClient) {
	s := &inspSrv{tmplInv: 1, srcInv: 3, stress: stressSpec(), cpu: []surveyQ{sq("target_hosts", "textarea", true), sq("intensity", "integer", false)},
		invs: []invInfo{{1, "DEFAULT"}, {2, "admin"}, {3, "src-inv"}, {9, "kim"}}}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(srv.Close)
	cfg, err := parseConfig(`
awx_url = `+srv.URL+`
username = admin
password = pw
inventory = 3
cpu_template = cpu-test
cpu_hostname_key = target_hosts
stress_template = 5
stress_hostname_key = target_hosts
`, "t.conf")
	if err != nil {
		t.Fatal(err)
	}
	return s, cfg, newAWXClient(srv.URL, "admin", "pw", false, 5*time.Second)
}

func runInsp(t *testing.T, cfg *Config, c *awxClient, args ...string) (int, string) {
	t.Helper()
	var b bytes.Buffer
	code := inspectRun(&b, cfg, c, args)
	return code, b.String()
}

func TestInspectRunOK(t *testing.T) {
	s, cfg, c := newInspect(t)
	code, out := runInsp(t, cfg, c)
	if code != 0 {
		t.Fatalf("정상 conf 인데 종료코드 %d\n%s", code, out)
	}
	for _, w := range []string{
		"===== 템플릿 cpu-test (id 6)", "===== 템플릿 stress-ng (id 5)", "default_inventory 와 같음", "ask_inventory_on_launch: true",
		"survey: 사용 (변수 7개)", "question_name", "min/max", "템플릿 extra_vars:", "foo: bar",
		"stress_end_keys end_year: survey type integer → 숫자로 보냄",
		"default_inventory", "username", "동기화 인벤토리: src-inv (id 3)", "source_path", "사용",
		"동기화 인벤토리(inventory=3)와 실행 때 템플릿에 붙일 인벤토리(username=admin, id 2)가 다릅니다",
		"stress_end_keys = end_year,end_month,end_day,end_hour,end_min", "cpu_template = cpu-test", "종료코드 0",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("출력에 %q 없음\n%s", w, out)
		}
	}
	if len(s.writes) != 0 {
		t.Errorf("읽기 전용인데 쓰기 요청: %v", s.writes)
	}
}

func TestInspectRunSameInventory(t *testing.T) {
	s, cfg, c := newInspect(t)
	s.srcInv = 3
	cfg.Username = "src-inv"
	if code, out := runInsp(t, cfg, c, "cpu-test"); code != 0 || !strings.Contains(out, "동기화 인벤토리와 username(src-inv) 인벤토리가 같음") {
		t.Errorf("code=%d\n%s", code, out)
	}
}

func TestInspectRunMismatch(t *testing.T) {
	// 키가 survey 에 없음 → 1
	_, cfg, c := newInspect(t)
	cfg.CPU.HostnameKey = "no_such"
	code, out := runInsp(t, cfg, c)
	if code != 1 || !strings.Contains(out, "cpu_hostname_key = no_such 가 survey 에 없습니다") || !strings.Contains(out, "종료코드 1") {
		t.Errorf("없는 키 code=%d\n%s", code, out)
	}
	// 필수 항목 누락 → 1
	s, cfg, c := newInspect(t)
	s.cpu = append(s.cpu, sq("extra_req", "text", true))
	if code, out = runInsp(t, cfg, c, "6"); code != 1 || !strings.Contains(out, "필수 survey 항목이 conf 에 없음: extra_req") {
		t.Errorf("필수 누락 code=%d\n%s", code, out)
	}
	// 템플릿 인벤토리가 DEFAULT 가 아님 → 1
	s, cfg, c = newInspect(t)
	s.tmplInv = 9
	if code, out = runInsp(t, cfg, c); code != 1 || !strings.Contains(out, "현재 인벤토리: kim (id 9)") || !strings.Contains(out, "사용 중일 수 있음") {
		t.Errorf("사용 중 code=%d\n%s", code, out)
	}
	// username / default 인벤토리 없음 → 1
	s, cfg, c = newInspect(t)
	cfg.Username, cfg.DefaultInventory = "nobody", "NODEF"
	if code, out = runInsp(t, cfg, c); code != 1 || !strings.Contains(out, "username(nobody) 과 같은 이름의 인벤토리가 AWX 에 없음") || !strings.Contains(out, "default_inventory(NODEF) 인벤토리가 AWX 에 없음") {
		t.Errorf("인벤토리 없음 code=%d\n%s", code, out)
	}
	// conf 템플릿이 아닌 템플릿 인자는 키 점검 없이 보이기만 → 0
	s, cfg, c = newInspect(t)
	cfg.CPU.Template, cfg.Stress.Template = "other", "other2"
	if code, out = runInsp(t, cfg, c, "6"); code != 0 || !strings.Contains(out, "키 점검·제안은 생략") {
		t.Errorf("conf 외 템플릿 code=%d\n%s", code, out)
	}
	// 소스가 없음 → 1
	s, cfg, c = newInspect(t)
	cfg.Inventory = "8"
	if code, out = runInsp(t, cfg, c, "6"); code != 1 || !strings.Contains(out, "등록된 소스가 없음") {
		t.Errorf("소스 없음 code=%d\n%s", code, out)
	}
	_ = s
}

func TestInspectRunInventorySource(t *testing.T) {
	// inventory_source 로 고정하면 그 소스가 속한 인벤토리를 본다
	_, cfg, c := newInspect(t)
	cfg.InventorySource, cfg.Inventory = "7", ""
	code, out := runInsp(t, cfg, c, "6")
	if code != 0 || !strings.Contains(out, "inventory_source 로 고정") {
		t.Errorf("code=%d\n%s", code, out)
	}
	// source_field 가 소스에 없음 → 1
	cfg.SourceField = "no_field"
	if code, out = runInsp(t, cfg, c, "6"); code != 1 || !strings.Contains(out, "source_field(no_field) 필드가 없음") {
		t.Errorf("source_field code=%d\n%s", code, out)
	}
}

func TestInspectRunErrors(t *testing.T) {
	// 접속 불가 → 2
	_, cfg, c := newInspect(t)
	bad := newAWXClient("http://127.0.0.1:1", "admin", "pw", false, time.Second)
	if code, out := runInsp(t, cfg, bad); code != 2 || !strings.Contains(out, "AWX 접속·조회 실패") {
		t.Errorf("접속 불가 code=%d\n%s", code, out)
	}
	// 템플릿 없음 → 2
	if code, out := runInsp(t, cfg, c, "no-such-template"); code != 2 || !strings.Contains(out, "템플릿 조회 실패") {
		t.Errorf("템플릿 없음 code=%d\n%s", code, out)
	}
	// 템플릿 인자도 conf 템플릿도 없음 → 2
	cfg.CPU.Template, cfg.Stress.Template = "", ""
	if code, out := runInsp(t, cfg, c); code != 2 || !strings.Contains(out, "조사할 템플릿이 없습니다") {
		t.Errorf("템플릿 없음2 code=%d\n%s", code, out)
	}
}

func TestTemplateRoles(t *testing.T) {
	cfg := &Config{CPU: TemplateConf{Template: "cpu-test"}, Stress: TemplateConf{Template: "5"}}
	if r := templateRoles(cfg, &jobTemplate{ID: 6, Name: "cpu-test"}); !reflect.DeepEqual(r, []string{stageCPU}) {
		t.Errorf("cpu = %v", r)
	}
	if r := templateRoles(cfg, &jobTemplate{ID: 5, Name: "stress-ng"}); !reflect.DeepEqual(r, []string{stageStress}) {
		t.Errorf("stress = %v", r)
	}
	if r := templateRoles(cfg, &jobTemplate{ID: 9, Name: "x"}); len(r) != 0 {
		t.Errorf("none = %v", r)
	}
}
