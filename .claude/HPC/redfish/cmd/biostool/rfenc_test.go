package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rfenc.go 시험 (2순위 계정 비밀번호 암호화 파일 rf.enc). 실제 BMC 는 쓰지 않는다.

const (
	rfEncPassDell  = `Dell#Enc/pw"1<>`
	rfEncPassHPE   = `Hpe$Enc'pw2!`
	rfEncPassDell2 = `Dell#Enc/pw"NEW`
	rfPlainDell    = `Plain#Dell'pw9`
)

// rfSecrets 는 출력·파일에 나오면 안 되는 비밀번호 목록입니다.
var rfSecrets = []string{rfEncPassDell, rfEncPassHPE, rfEncPassDell2, rfPlainDell}

func assertNoRFSecrets(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, p := range rfSecrets {
			if strings.Contains(s, p) {
				t.Errorf("%s 에 비밀번호가 나옴", what)
			}
		}
	}
}

func checkPerm600(t *testing.T, p string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stat(p)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("%s 권한 %04o, 0600 이어야 함", p, fi.Mode().Perm())
	}
}

// 벤더 둘을 저장한 뒤 하나만 갱신해도 나머지는 그대로이고, 파일은 0600·평문 없음.
func TestRFEncSaveKeepsOtherVendors(t *testing.T) {
	dir := t.TempDir()
	enc, key := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin")
	must(t, saveRFEncVendor(enc, key, "dell", rfEncPassDell))
	must(t, saveRFEncVendor(enc, key, "hpe", rfEncPassHPE))
	checkPerm600(t, enc)
	checkPerm600(t, key)
	before, err := os.ReadFile(enc)
	must(t, err)
	hpeLine := ""
	for _, l := range strings.Split(string(before), "\n") {
		if strings.HasPrefix(l, "hpe=") {
			hpeLine = l
		}
	}
	if hpeLine == "" {
		t.Fatalf("hpe 줄 없음")
	}

	must(t, saveRFEncVendor(enc, key, "dell", rfEncPassDell2))
	after, err := os.ReadFile(enc)
	must(t, err)
	assertNoRFSecrets(t, "rf.enc", string(after))
	if !strings.Contains(string(after), hpeLine) {
		t.Error("dell 갱신 후 hpe 줄이 바뀌면 안 됨")
	}
	if strings.Count(string(after), "dell=") != 1 {
		t.Errorf("dell 줄은 하나여야 함:\n%d", strings.Count(string(after), "dell="))
	}
	checkPerm600(t, enc)

	m, warn, err := loadRFEnc(enc, key)
	must(t, err)
	if warn != "" {
		t.Errorf("0600 인데 경고: %s", warn)
	}
	if m["dell"] != rfEncPassDell2 || m["hpe"] != rfEncPassHPE || len(m) != 2 {
		t.Errorf("복호화 결과 불일치 (벤더 %d개)", len(m))
	}
	// 임시 파일이 남지 않음
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".rf.enc.tmp") {
			t.Errorf("임시 파일이 남음: %s", e.Name())
		}
	}
}

// 잘못된 벤더 이름은 저장·해석 모두 거부하고, 대소문자·공백은 정규화한다.
func TestRFEncRejectsBadVendor(t *testing.T) {
	dir := t.TempDir()
	enc, key := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin")
	for _, v := range []string{"", "ibm", "dell2", "DELL"} {
		if err := saveRFEncVendor(enc, key, v, rfEncPassDell); err == nil {
			t.Errorf("벤더 %q 가 거부되지 않음", v)
		}
	}
	if _, err := os.Stat(enc); !os.IsNotExist(err) {
		t.Error("거부 시 rf.enc 가 생기면 안 됨")
	}
	if err := saveRFEncVendor(enc, key, "dell", ""); err == nil {
		t.Error("빈 비밀번호가 거부되지 않음")
	}
	if normRFVendor(" Dell ") != "dell" {
		t.Error("normRFVendor")
	}
	// 파일 안의 알 수 없는 벤더·깨진 암호문·중복은 오류 (값은 메시지에 없음)
	for name, text := range map[string]string{
		"벤더":  "ibm=" + rfEncPassDell + "\n",
		"형식":  rfEncPassDell + "\n",
		"암호문": "dell=" + rfEncPassDell + "\n",
		"중복":  "dell=AAAA\ndell=AAAA\n",
	} {
		_, err := parseRFEnc(text, "rf.enc")
		if err == nil {
			t.Errorf("%s: 오류가 나야 함", name)
			continue
		}
		assertNoRFSecrets(t, name+" 오류", err.Error())
	}
}

// 파이프 입력으로 encryptRFVendor(= biostool encrypt -rf) 가 그 벤더만 저장하고 안내에 비밀번호가 없음.
func TestEncryptRFVendorPipe(t *testing.T) {
	dir := t.TempDir()
	enc, key := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin")
	feed := func(v, pw string) error {
		r, w, err := os.Pipe()
		must(t, err)
		_, _ = w.WriteString(pw + "\n")
		w.Close()
		defer r.Close()
		var prompt bytes.Buffer
		err = encryptRFVendor(r, &prompt, v, enc, key)
		assertNoRFSecrets(t, "안내", prompt.String())
		return err
	}
	must(t, feed("Dell", rfEncPassDell))
	must(t, feed("hpe", rfEncPassHPE))
	if err := feed("ibm", rfEncPassDell2); err == nil || !strings.Contains(err.Error(), "알 수 없는 벤더") {
		t.Errorf("잘못된 벤더 오류 = %v", err)
	}
	m, _, err := loadRFEnc(enc, key)
	must(t, err)
	if m["dell"] != rfEncPassDell || m["hpe"] != rfEncPassHPE {
		t.Error("파이프로 저장한 값 불일치")
	}
}

// 다른 키로 만든 rf.enc 에는 덧쓰지 않고, 키 없음·키 불일치는 비밀번호 없이 오류.
func TestRFEncKeyMismatch(t *testing.T) {
	dir := t.TempDir()
	enc, key := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin")
	must(t, saveRFEncVendor(enc, key, "dell", rfEncPassDell))
	orig, _ := os.ReadFile(enc)

	other := filepath.Join(dir, "other.bin")
	if _, err := loadOrCreateKey(other); err != nil {
		t.Fatal(err)
	}
	if err := saveRFEncVendor(enc, other, "hpe", rfEncPassHPE); err == nil {
		t.Error("다른 키로 덧쓰기가 거부되지 않음")
	}
	if err := saveRFEncVendor(enc, filepath.Join(dir, "없음.bin"), "hpe", rfEncPassHPE); err == nil {
		t.Error("기존 항목이 있는데 키가 없으면 거부해야 함 (새 키를 만들면 기존 항목을 못 품)")
	}
	if now, _ := os.ReadFile(enc); !bytes.Equal(now, orig) {
		t.Error("거부했는데 rf.enc 가 바뀜")
	}
	if _, err := os.Stat(filepath.Join(dir, "없음.bin")); !os.IsNotExist(err) {
		t.Error("거부 시 키 파일이 생기면 안 됨")
	}
	for _, k := range []string{other, filepath.Join(dir, "없음.bin")} {
		_, _, err := loadRFEnc(enc, k)
		if err == nil {
			t.Errorf("키 %s 로 복호화 오류가 나야 함", filepath.Base(k))
			continue
		}
		assertNoRFSecrets(t, "복호화 오류", err.Error())
	}
	// 없는 rf.enc 는 오류 아님
	if m, _, err := loadRFEnc(filepath.Join(dir, "없음.enc"), key); err != nil || m != nil {
		t.Errorf("없는 rf.enc: m=%v err=%v", m != nil, err)
	}
	if _, _, err := loadRFEnc(dir, key); err == nil {
		t.Error("폴더는 오류여야 함")
	}
}

// rf.conf(계정명) + rf.enc(비밀번호): rf.enc 우선, 평문 줄은 경고 한 줄(값 없음) 후 호환.
func TestLoadRFConfEncPriorityAndPlainWarn(t *testing.T) {
	dir := t.TempDir()
	enc, key, conf := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin"), filepath.Join(dir, "rf.conf")
	must(t, saveRFEncVendor(enc, key, "dell", rfEncPassDell))
	must(t, saveRFEncVendor(enc, key, "hpe", rfEncPassHPE))
	must(t, saveRFEncVendor(enc, key, "lenovo", rfEncPassDell2)) // rf.conf 에 lenovo_user 가 없으면 쓰이지 않음
	must(t, os.WriteFile(conf, []byte("dell_user="+fbUser+"\ndell_pass="+rfPlainDell+"\nhpe_user=hpeadm\n"), 0o600))

	fb, warns, err := loadRFConfEnc(conf, enc, key)
	must(t, err)
	if u, p, ok := fb.lookup("dell"); !ok || u != fbUser || p != rfEncPassDell {
		t.Error("dell: rf.enc 가 평문보다 우선해야 함")
	}
	if u, p, ok := fb.lookup("hpe"); !ok || u != "hpeadm" || p != rfEncPassHPE {
		t.Error("hpe: rf.enc 비밀번호로 읽혀야 함")
	}
	if _, _, ok := fb.lookup("lenovo"); ok {
		t.Error("lenovo_user 가 없으면 예비 계정 없음")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "dell_pass") || strings.Contains(warns[0], "\n") {
		t.Errorf("평문 경고 한 줄이어야 함: %q", warns)
	}
	assertNoRFSecrets(t, "경고", strings.Join(warns, "\n"))

	// rf.enc 가 없으면: hpe_user 는 비밀번호가 어디에도 없어 오류
	if _, _, err := loadRFConfEnc(conf, filepath.Join(dir, "없음.enc"), key); err == nil || !strings.Contains(err.Error(), "hpe_pass") {
		t.Errorf("hpe 비밀번호 없음 오류 = %v", err)
	}
	// rf.enc 가 없고 평문만 있으면 평문으로 동작 (경고 유지)
	must(t, os.WriteFile(conf, []byte("dell_user="+fbUser+"\ndell_pass="+rfPlainDell+"\n"), 0o600))
	fb, warns, err = loadRFConfEnc(conf, filepath.Join(dir, "없음.enc"), key)
	must(t, err)
	if _, p, ok := fb.lookup("dell"); !ok || p != rfPlainDell {
		t.Error("rf.enc 없음: 평문 호환이어야 함")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "dell_pass") {
		t.Errorf("평문 경고 = %q", warns)
	}
	assertNoRFSecrets(t, "경고", strings.Join(warns, "\n"))
}

// 비밀번호가 어디에도 없으면 오류(안내 포함), rf.conf 가 없으면 rf.enc 를 읽지 않고 기능 꺼짐, rf.enc 권한 경고.
func TestLoadRFConfEncMissingAndPerm(t *testing.T) {
	dir := t.TempDir()
	enc, key, conf := filepath.Join(dir, "rf.enc"), filepath.Join(dir, "key.bin"), filepath.Join(dir, "rf.conf")
	must(t, os.WriteFile(conf, []byte("cisco_user="+fbUser+"\n"), 0o600))
	_, _, err := loadRFConfEnc(conf, enc, key)
	if err == nil || !strings.Contains(err.Error(), "cisco_pass 가 비어") || !strings.Contains(err.Error(), "encrypt.sh rf cisco") {
		t.Errorf("비밀번호 없음 오류 = %v", err)
	}

	// rf.conf 없음: 깨진 rf.enc 가 있어도 읽지 않는다
	must(t, os.WriteFile(enc, []byte("ibm=xx\n"), 0o600))
	fb, warns, err := loadRFConfEnc(filepath.Join(dir, "없음.conf"), enc, key)
	if fb != nil || warns != nil || err != nil {
		t.Errorf("rf.conf 없음: fb=%v warns=%q err=%v", fb != nil, warns, err)
	}
	// rf.conf 있음 + 깨진 rf.enc → 오류
	if _, _, err := loadRFConfEnc(conf, enc, key); err == nil {
		t.Error("깨진 rf.enc 는 오류여야 함")
	}

	must(t, os.Remove(enc))
	must(t, saveRFEncVendor(enc, key, "cisco", rfEncPassDell))
	if runtime.GOOS != "windows" {
		must(t, os.Chmod(enc, 0o644))
		_, warns, err = loadRFConfEnc(conf, enc, key)
		must(t, err)
		if len(warns) != 1 || !strings.Contains(warns[0], "chmod 600") {
			t.Errorf("rf.enc 권한 경고 = %q", warns)
		}
		// 다시 저장하면 0600 으로 돌아온다
		must(t, saveRFEncVendor(enc, key, "cisco", rfEncPassHPE))
		checkPerm600(t, enc)
	}
	fb, warns, err = loadRFConfEnc(conf, enc, key)
	must(t, err)
	if _, p, ok := fb.lookup("cisco"); !ok || p != rfEncPassHPE || len(warns) != 0 {
		t.Errorf("cisco rf.enc 읽기 실패 (warns=%q)", warns)
	}
	// fmt 로 찍어도 비밀번호가 나오지 않음 (클로저)
	assertNoRFSecrets(t, "fallbackCreds 출력", fmt.Sprintf("%v %+v %#v", fb, fb, fb))
}

// prepare 가 bios.conf 의 rf_enc_file·key_file 로 rf.enc 를 읽고, 평문 경고를 비밀번호 없이 한 줄 찍는다.
func TestPrepareReadsRFEnc(t *testing.T) {
	dir := t.TempDir()
	pass, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	must(t, encryptFromReader(strings.NewReader(testPass), pass, key))
	enc := filepath.Join(dir, "현장.enc")
	must(t, saveRFEncVendor(enc, key, "dell", rfEncPassDell))
	conf := filepath.Join(dir, "bios.conf")
	must(t, os.WriteFile(conf, []byte("user="+testUser+"\npass_file="+pass+"\nkey_file="+key+"\nrf_enc_file="+enc+"\n"), 0o600))
	rfc := filepath.Join(dir, "rf.conf")
	must(t, os.WriteFile(rfc, []byte("dell_user="+fbUser+"\nhpe_user=h\nhpe_pass="+rfPlainDell+"\n"), 0o600))

	var warnBuf bytes.Buffer
	old := prepareWarnOut
	prepareWarnOut = &warnBuf
	defer func() { prepareWarnOut = old }()
	rc, err := prepare(&commonOpts{conf: conf, rfConf: rfc, hosts: writeHosts(t, "")}, "127.0.0.1", "")
	must(t, err)
	if _, p, ok := rc.Fallback.lookup("dell"); !ok || p != rfEncPassDell {
		t.Error("prepare: rf.enc 의 dell 비밀번호가 읽혀야 함")
	}
	if _, p, ok := rc.Fallback.lookup("hpe"); !ok || p != rfPlainDell {
		t.Error("prepare: 평문 hpe 호환")
	}
	w := warnBuf.String()
	if strings.Count(w, "경고:") != 1 || !strings.Contains(w, "hpe_pass") {
		t.Errorf("경고 출력 = %q", w)
	}
	assertNoRFSecrets(t, "prepare 경고", w)
}
