# mn

`mn` — 자주 쓰는 디렉토리·툴 사용법 가이드 + 현재 쉘에서 바로 cd

## 면책 문구

이 도구는 **제공 그대로(as is)** 배포됩니다. 운영 환경에 반영하기 전에 반드시 직접 검증하세요. 사용에 따른 결과의 책임은 사용자에게 있습니다.

## 설치

```bash
git clone <저장소 주소>     # 또는 폴더를 통째로 다운로드
cd mn
sudo ./install.sh
```

- 빌드가 필요 없습니다. bash 4.1 이상만 있으면 되며, 폐쇄망에서도 동작합니다.
- 설치 후 **새 로그인 쉘**을 열어야 적용됩니다.
- `/etc/dir/dir.conf` 가 이미 있으면 덮어쓰지 않고 그대로 유지합니다.
- 제거: `sudo ./install.sh --uninstall` (`dir.conf` 는 남깁니다.)

## 사용법

| 명령 | 설명 |
|---|---|
| `mn` | 전체 목록 출력 |
| `mn 2` | 2번 경로로 이동 (현재 쉘에서 cd) |
| `mn ldap` | 이름으로 찾아 이동 |
| `mn add <섹션> <이름> <설명>` | 항목 추가 |
| `mn edit` | `dir.conf` 편집 |
| `mn -h` | 도움말 |

- `MN_CONF` 환경변수로 다른 conf 파일을 지정할 수 있습니다. 예: `MN_CONF=/path/other.conf mn`
- 색상은 터미널에서 실행할 때만 출력됩니다. `NO_COLOR` 환경변수를 설정하면 끕니다.

## dir.conf 형식

```
[HPC]
/path/OS_config_check | os체크 스크립트
    여러 줄 설명은 공백으로 시작하는 줄로 이어 씁니다.

[툴사용법/공통]
gossh | gossh -w a "uptime" 처럼 사용, 기타 옵션
```

규칙:

- `[섹션]`, `[섹션/하위]` 헤더로 그룹을 나눕니다.
- `이름 | 설명` 한 줄이 항목 하나입니다.
- 이름이 `/` 또는 `~` 로 시작하면 경로 항목입니다. 번호가 붙고 `mn 번호` 로 이동합니다. 그 외는 일반 항목(툴/명령/메모)입니다.
- 공백으로 시작하는 줄은 바로 앞 항목의 설명에 이어 붙습니다.
- `#` 주석과 빈 줄은 무시합니다.

## 예상 출력

```
HPC
 - [1] /path/OS_config_check
      |_ os체크 스크립트
 - [2] /path/NEW_LDAP_CH
      |_ ldap변경 스크립트 (구버전)
VM
 - [3] /path/VMsetup
      |_ 설정 의존 디렉토리, BM→VM 전환 시 사용
툴사용법
 공통
   - gossh
        |_ gossh -w a "uptime" 처럼 사용, 기타 옵션
 VM
   - vm_host
        |_ vm list 출력
```

## 동작 원리

스크립트는 자식 프로세스로 실행되므로 부모 쉘의 현재 디렉토리를 바꿀 수 없습니다. 그래서 `mn` 은 실행할 때마다 `~/.mn_cd` 를 다시 만듭니다. 이동할 경로가 있으면 `cd "경로"` 한 줄을 쓰고, 없으면 빈 파일로 둡니다. `/etc/profile.d` 의 래퍼가 mn 실행 직후 이 파일을 source 합니다.

csh/tcsh (`mn.csh`):

```csh
alias mn '/usr/local/bin/mn \!* ; source $HOME/.mn_cd'
```

bash (`mn.sh`):

```bash
mn() { /usr/local/bin/mn "$@"; . "$HOME/.mn_cd"; }
```

## 파일 구성

- `mn` — 본체 스크립트 (`/usr/local/bin/mn` 으로 설치)
- `dir.conf.sample` — `dir.conf` 샘플 (`/etc/dir/dir.conf` 가 없을 때 복사)
- `profile.d/mn.sh` — bash 함수 (`/etc/profile.d/mn.sh` 로 설치)
- `profile.d/mn.csh` — csh/tcsh alias (`/etc/profile.d/mn.csh` 로 설치)
- `install.sh` — 설치 / `--uninstall` 제거
