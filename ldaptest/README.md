# ldaptest — auto_setup LDAP 백업·변환·복원 확인 도구

auto_setup 이 OS 재설치 때 쓰는 LDAP 백업·RHEL7↔8 변환·복원 로직을 **그대로** 실행해 결과를 눈으로 확인하는 도구입니다.
`./test` 는 `auto_setup ldap-test` 를 부르는 래퍼이며, 수집·저장·변환·비교·복원은 auto_setup 데몬과 같은 함수
(`collectHosts`·`convertEntriesFor`·`compareEntries`·`restoreEntries`)를 씁니다. job·데몬·`/tmp/auto_setup/ldapbak` 과는 무관합니다.

## 사용법

```bash
cd ldaptest

# list 의 서버마다 백업 + RHEL7/RHEL8 용 변환 세트를 ./out/<호스트>/ 에 만든다
./test list

# list 의 서버(정확히 1대) 백업으로 target 을 비교하고, 필요하면 복원한다 (yes 확인)
./test list target

# 결과 디렉터리 변경 / 복원 확인 생략
./test -out /root/ldap_out list
./test -y list target
```

- 실행 파일은 이 스크립트 위치 기준 `../auto_setup_os8`, 없으면 PATH 의 `auto_setup` 을 씁니다(`ldap-test` 가 들어간 빌드여야 함).
- `./out` 은 **실행한 위치** 기준입니다(래퍼가 작업 디렉터리를 바꾸지 않음).
- 같은 호스트를 다시 실행하면 `out/<호스트>/` 를 비우고 다시 만듭니다(다른 호스트 결과는 그대로).
- 수집 경로는 auto_setup 과 같습니다: 기본 os8 gossh, 응답이 없으면 os6_mgmt 경유(`os6_mgmt`·`os6_gossh` 설정 시).
- s4 호스트 규칙은 auto_setup 과 같은 환경변수(`AUTO_SETUP_LDAP_S4_PREFIX`·`AUTO_SETUP_LDAP_S4_SERVICES`)를 따릅니다.
- 종료코드: `0` 모두 성공, `1` 일부 실패(수집·변환·비교·복원 실패, 복원 취소, 서비스 재시작 실패), `2` 사용법 오류.

## list 형식

한 줄에 호스트 하나. 공백·쉼표·탭으로 여러 개를 적어도 되고, `#` 뒤는 주석, 빈 줄은 무시, 중복은 한 번만 처리합니다.

```
# 테스트 대상
svr001
svr002, svr003
```

## 결과 디렉터리

```
out/<호스트>/                         (0700)
  backup/                             수집한 원본 그대로 + meta.json (auto_setup 전달 시점 백업과 같은 형식)
    ldap.conf resolv.conf autofs.conf autofs_ldap_auth.conf (있는 것만)
    nslcd.conf ntp.conf               (OS7 이하)  또는  sssd.conf chrony.conf (OS8 이상)
    meta.json                         호스트·OS·auth/time·파일별 원격 경로·mode·owner·크기 (비밀값 없음)
  rhel7/                              RHEL7(nslcd/ntp) 에 쓸 세트 — 공통 파일 + nslcd.conf ntp.conf
    files.txt                         이름·원격 경로·mode·owner·group·크기 (비밀값 없음)
    notes.txt                         '변환 없음(원본 그대로)' 또는 '변환: sssd.conf→nslcd.conf, chrony.conf→ntp.conf'
  rhel8/                              RHEL8(sssd/chrony) 에 쓸 세트 — 공통 파일 + sssd.conf chrony.conf
    files.txt notes.txt
```

- 파일은 모두 0600 입니다. 공통 파일(ldap.conf·resolv.conf·autofs.conf·autofs_ldap_auth.conf)은 backup·rhel7·rhel8 이 같습니다.
- 변환은 auto_setup 복원과 같습니다(7→8: nslcd.conf→sssd.conf, ntp.conf→chrony.conf / 8→7: 반대). 변환 파일은 root:root, sssd/nslcd 0600·chrony/ntp 0644 로 기록됩니다.
- 변환에 필요한 값(uri·binddn·bindpw, server/pool)이 없으면 그 세트는 `FAIL(변환 실패: <파일>)` 이고 디렉터리를 만들지 않습니다.
- 수집 실패(무응답 등)·OS 판별 불가·ldap.conf 없음이면 `out/<호스트>/` 는 빈 디렉터리로 남습니다.

## target 모드

`./test list target` 은 list 의 서버(정확히 1대, 아니면 오류) 백업으로 target 을 **auto_setup `request ldap-restore` 와 같은 규칙**으로 처리합니다.

1. 비교(읽기 전용): 백업 ldap.conf 의 binddn uid·auth/time 종류를 target 과 비교. **같으면 복원하지 않고 끝**(종료코드 0).
2. 복원 조건(binddn uid 다름 · OS 종류 변경 · target 에 binddn 없음)이면 target 의 OS 판별에 따라 7/8 변환한 세트와 복원할 파일 목록을 보여 주고 `yes` 입력을 기다립니다(`-y` 면 생략).
3. 복원: `ldap_share_dir` 공유경로를 거쳐 파일을 쓰고(gossh 명령줄에 비밀값 없음, 공유 임시 디렉터리는 즉시 삭제), `systemctl restart sssd autofs chronyd nscd`(OS7 이하 `nslcd autofs ntpd nscd`, 설치 안 된 유닛은 skip, enable·authselect 안 함) 후 binddn uid 를 다시 확인합니다.
4. 화면: 비교 사유, 변환 방향, 파일별 OK/FAIL, 서비스별 ok/skip/err, 복원 후 binddn 확인, 최종 결과.

`ldap_share_dir` 이 conf 에 없으면 auto_setup 과 같이 `수동 복원 필요(ldap_share_dir 미설정, 백업: out/<호스트>/backup)` 로 끝나고 아무것도 쓰지 않습니다.

## 주의

- **`out/` 에는 bindpw·secret 이 평문으로 들어 있습니다**(backup·rhel7·rhel8 의 설정 파일). 0700/0600 으로 만들지만 복사·보관에 주의하고, 확인이 끝나면 `rm -rf out` 으로 지우십시오. 화면·files.txt·notes.txt·meta.json 에는 비밀값을 쓰지 않습니다.
- **target 모드는 실제 서버의 설정 파일을 덮어쓰고 서비스를 재시작합니다.** 테스트 서버에만 쓰십시오.
- target 복원에는 conf `ldap_share_dir`(os8_mgmt 와 target 이 모두 보는 autofs 공유경로, target root 가 읽을 수 있어야 함)이 필요합니다.

## 예시 출력

```
$ ./test list
h7                   OS7  nslcd/ntp    backup OK  rhel7 OK  rhel8 OK  → out/h7
h8                   OS8  sssd/chrony  backup OK  rhel7 OK  rhel8 OK  → out/h8
hnone                OS?  -/-          backup FAIL(무응답)  rhel7 FAIL(백업 없음)  rhel8 FAIL(백업 없음)  → out/hnone
합계: 3대 — 성공 2, 실패 1 (backup 실패 1, rhel7 실패 1, rhel8 실패 1) → out

$ ./test list8 t7          # list8 에는 h8 한 대
h8                   OS8  sssd/chrony  backup OK  rhel7 OK  rhel8 OK  → out/h8
합계: 1대 — 성공 1, 실패 0 (backup 실패 0, rhel7 실패 0, rhel8 실패 0) → out

== target t7 : h8 의 백업으로 비교 (auto_setup 과 같은 규칙) ==
비교: 복원 필요 — binddn uid 다름
변환: 8→7 (sssd.conf→nslcd.conf,chrony.conf→ntp.conf)
복원할 파일:
  resolv.conf            /etc/resolv.conf           644 root:root
  ldap.conf              /etc/openldap/ldap.conf    644 root:root
  autofs.conf            /etc/autofs.conf           644 root:root
  autofs_ldap_auth.conf  /etc/autofs_ldap_auth.conf 600 root:root
  nslcd.conf             /etc/nslcd.conf            600 root:root
  ntp.conf               /etc/ntp.conf              644 root:root
t7 의 위 파일을 덮어쓰고 서비스(sssd|nslcd autofs chronyd|ntpd nscd)를 재시작합니다. 계속하려면 yes 입력: yes
파일:
  resolv.conf            OK
  ldap.conf              OK
  autofs.conf            OK
  autofs_ldap_auth.conf  OK
  nslcd.conf             OK
  ntp.conf               OK
서비스 재시작: nslcd ok, autofs ok, ntpd ok, nscd ok
복원 후 binddn uid: 백업과 같음
결과: 복원 완료 (변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf)
```
