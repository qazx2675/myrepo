package dhcp

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// 같은 대역 파일은 hostname 수만큼 다시 읽지 않고 한 번만 파싱해야 한다.
// 첫 호출 뒤 파일을 지워버려도 같은 결과가 나오면 캐시가 실제로 쓰인 것이다.
func TestParseFileCachedReadsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "10.10.10")
	body := "host svr01ev01 { hardware ethernet 00:11:22:33:44:55; fixed-address 10.10.10.11; }\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := parseFileCached(path)
	if err != nil {
		t.Fatalf("첫 파싱 실패: %v", err)
	}
	if first["svr01ev01"].MAC != "00:11:22:33:44:55" {
		t.Fatalf("첫 파싱 결과가 이상함: %+v", first)
	}

	// 파일을 지운다 — 캐시가 없으면 두 번째 호출은 반드시 실패한다.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	second, err := parseFileCached(path)
	if err != nil {
		t.Fatalf("캐시가 동작하지 않아 두 번째 호출이 실패함: %v", err)
	}
	if second["svr01ev01"].MAC != "00:11:22:33:44:55" {
		t.Errorf("캐시된 결과가 다름: %+v", second)
	}
}

// 여러 goroutine이 동시에 같은 파일을 조회해도 안전해야 한다(Resolve가 병렬로 불린다).
func TestParseFileCachedConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "10.20.30")
	body := "host svr02ev01 { hardware ethernet aa:bb:cc:dd:ee:ff; fixed-address 10.20.30.5; }\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs, err := parseFileCached(path)
			if err != nil {
				t.Errorf("파싱 실패: %v", err)
				return
			}
			if recs["svr02ev01"].MAC != "aa:bb:cc:dd:ee:ff" {
				t.Errorf("결과가 다름: %+v", recs)
			}
		}()
	}
	wg.Wait()
}
