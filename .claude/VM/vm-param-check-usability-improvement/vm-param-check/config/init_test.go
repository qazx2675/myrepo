package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFolderBlank(t *testing.T) {
	root := t.TempDir()

	specFile, err := InitFolder(root, "TST-CAE001-SAMP48c-QRST", "")
	if err != nil {
		t.Fatalf("InitFolder 실패: %v", err)
	}
	if _, err := os.Stat(specFile); err != nil {
		t.Fatalf("스펙 파일이 생성되지 않았습니다: %v", err)
	}

	data, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cores=") {
		t.Errorf("빈 틀에 cores= 가 없습니다:\n%s", data)
	}

	// 방금 만든 스펙이 실제로 매칭되는지 확인(자기 자신을 못 찾으면 스캐폴드 의미가 없다).
	// 필수 옵션들은 값이 비어 있는 채로("cores=") 남는데, 이건 문법상으로는 유효한
	// 이름=값 쌍이라 ParseSpecFile 단계에서는 걸러지지 않는다 — 실제로 값을 안 채우고
	// 쓰면 나중에 vm-param-check가 flag.Set("cores", "")에서 정수 변환 실패로 막는다.
	m, err := FindSpec(root, "TST-CAE007-SAMP48c-QRST")
	if err != nil {
		t.Fatalf("생성된 빈 틀을 다시 찾는 데 실패했습니다: %v", err)
	}
	if m == nil {
		t.Fatal("생성한 빈 틀 스펙을 못 찾았습니다")
	}
	for _, o := range m.Options {
		if o.Name == "cores" && o.Value != "" {
			t.Errorf("빈 틀인데 cores 값이 이미 채워져 있습니다: %q", o.Value)
		}
	}
}

func TestInitFolderDuplicateRejected(t *testing.T) {
	root := t.TempDir()
	if _, err := InitFolder(root, "TST-CAE001-SAMP48c-QRST", ""); err != nil {
		t.Fatal(err)
	}
	// 차수만 다른 이름으로 다시 만들면(같은 스펙으로 매칭됨) 막혀야 한다.
	if _, err := InitFolder(root, "TST-CAE009-SAMP48c-QRST", ""); err == nil {
		t.Error("이미 있는 스펙과 같은 이름(차수만 다름)인데 덮어쓰기가 막히지 않았습니다")
	}
}

func TestInitFolderRejectsBadName(t *testing.T) {
	root := t.TempDir()
	if _, err := InitFolder(root, "Task", ""); err == nil {
		t.Error("CAE 규칙에 안 맞는 이름인데 에러가 나지 않았습니다")
	}
}

func TestInitFolderWithTemplate(t *testing.T) {
	root := t.TempDir()

	// 기존 스펙(값 채워짐 + affinity 파일 참조) 준비
	srcDir := filepath.Join(root, "DEV-CAE001-SAMB16c-UVWX")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "affinity-ev02.txt"), []byte("sched.vcpu0.affinity = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcSpec := "ht=off\ncores=16\nnuma=16\ncpu=32\nmem=8\ndisk=50\nshares-ev01=2000\naffinity-ev02=affinity-ev02.txt\n"
	if err := os.WriteFile(filepath.Join(srcDir, "DEV-CAE001-SAMB16c-UVWX_spec.txt"), []byte(srcSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	specFile, err := InitFolder(root, "DEV-CAE002-SAMB32c-UVWX", "DEV-CAE009-SAMB16c-UVWX")
	if err != nil {
		t.Fatalf("InitFolder(템플릿 사용) 실패: %v", err)
	}

	m, err := FindSpec(root, "DEV-CAE005-SAMB32c-UVWX")
	if err != nil {
		t.Fatalf("생성된 스펙 재조회 실패: %v", err)
	}
	if m == nil {
		t.Fatal("템플릿으로 생성된 스펙을 다시 찾지 못했습니다")
	}
	want := map[string]string{"ht": "off", "cores": "16", "cpu": "32", "mem": "8", "disk": "50", "shares-ev01": "2000"}
	got := map[string]string{}
	for _, o := range m.Options {
		got[o.Name] = o.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("옵션 %s = %q, 템플릿에서 복사된 값 %q 을 기대했습니다", k, got[k], v)
		}
	}

	// affinity 파일도 새 디렉터리로 복사됐어야 한다(옛 디렉터리를 계속 가리키면 안 됨).
	copiedAffinity := filepath.Join(filepath.Dir(specFile), "affinity-ev02.txt")
	if _, err := os.Stat(copiedAffinity); err != nil {
		t.Errorf("affinity 파일이 새 스펙 디렉터리로 복사되지 않았습니다: %v", err)
	}
}
