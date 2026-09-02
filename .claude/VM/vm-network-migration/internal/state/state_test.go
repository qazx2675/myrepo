package state

import (
	"path/filepath"
	"testing"
	"time"
)

func sample() *File {
	return &File{
		User:      "hong",
		CreatedAt: time.Now(),
		NicIndex:  0,
		Records: []Record{
			{VMName: "vm-a", VMUUID: "uuid-a", OrigPG: "OLD_A", TargetPG: "NEW", NicKey: 4000},
			{VMName: "vm-b", VMUUID: "uuid-b", OrigPG: "OLD_B", TargetPG: "NEW", NicKey: 4000},
			{VMName: "vm-c", VMUUID: "uuid-c", OrigPG: "OLD_C", TargetPG: "NEW", NicKey: 4000},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	in := sample()
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Records) != 3 || out.User != "hong" {
		t.Fatalf("복원 결과가 다릅니다: %+v", out)
	}
	if out.Records[0].OrigPG != "OLD_A" || out.Records[0].NicKey != 4000 {
		t.Errorf("레코드 내용 불일치: %+v", out.Records[0])
	}
}

func TestLoadRejectsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.json")
	if err := Save(p, &File{User: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("레코드가 없는데 에러가 나지 않았습니다")
	}
}

func TestFilter(t *testing.T) {
	f := sample()

	all, err := f.Filter(nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("빈 목록은 전체를 돌려줘야 합니다: %d %v", len(all), err)
	}

	got, err := f.Filter([]string{"VM-B"}) // 대소문자 무시
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].VMName != "vm-b" {
		t.Fatalf("선택 필터 오류: %+v", got)
	}

	if _, err := f.Filter([]string{"vm-a", "없는vm"}); err == nil {
		t.Error("상태 파일에 없는 VM 을 지정했는데 에러가 나지 않았습니다")
	}
}

func TestRemove(t *testing.T) {
	f := sample()

	if n := f.Remove(nil); n != 0 || len(f.Records) != 3 {
		t.Fatalf("빈 목록은 아무것도 제거하지 않아야 합니다: n=%d len=%d", n, len(f.Records))
	}

	n := f.Remove([]string{"VM-A", "vm-c"}) // 대소문자 무시
	if n != 2 {
		t.Fatalf("제거 수 %d, want 2", n)
	}
	if len(f.Records) != 1 || f.Records[0].VMName != "vm-b" {
		t.Fatalf("남은 레코드가 잘못됐습니다: %+v", f.Records)
	}
}
