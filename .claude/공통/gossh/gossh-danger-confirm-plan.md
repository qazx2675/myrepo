# gossh 위험 작업 확인 강화 · 속도 최적화 · 배포 구조 정리 계획서

## 목표 및 범위
- `-dnlgjawkrdjqghkrdls`(위험 작업 강제 실행) 흐름에 **대상 호스트 재입력 대조** 단계를 넣고, 위험 명령 목록을 넓힌다.
- 기존 기능은 그대로 두고 **실행 속도**를 최적화한다(측정으로 10% 이상 빨라질 때만 반영).
- 저장소에서 `vendor/` 를 빼고, 빌드된 바이너리 `gossh_os6`·`gossh_os8` 로 배포한다.
- `workflow.svg` 를 auto_setup 흐름도 양식으로 다시 그린다.
- **제외**: auto_setup 수정(사용자 결정 — auto_setup 에서는 위험 작업을 실행하지 않는 방향), `y/N` → `Yes` 변경(auto_setup 이 같은 플래그를 쓰므로 유지), 옵션별 SVG.

## 확정 사항 (2026-10-31 인터뷰)
1. **위험어 추가 A·B·C 전부** — 기존 7개(`reboot` `poweroff` `shutdown` `halt` `init 0` `init 6` `ddc`)는 지금처럼 부분일치로 유지. 새 위험어는 **독립된 명령 단어일 때만** 감지(앞뒤가 문자열 끝·공백·`; | & ( ) \` ' "` 등) — 호스트명(`departed01`)·base64 데이터에 우연히 섞인 글자로 오탐하지 않게.
   - A 즉시 재시작·정지: `kexec`, `systemctl kexec|rescue|emergency`, `isolate rescue.target|emergency.target`, `init 1`/`telinit 1`, `sysrq-trigger`, `ipmitool ... power off|reset|cycle`
   - B 디스크·데이터 파괴: `mkfs*`, `dd ... of=/dev/`, `wipefs`, `fdisk`, `parted`, `sgdisk`, `shred`, `rm -rf /`(루트 자체), `lvremove`, `vgremove`, `pvremove`
   - C 접속 끊김: `ifdown`, `nmcli ... down`, `ip link set ... down`, `systemctl stop sshd|network|NetworkManager`
2. **호스트 대조 규칙** — `01.AWX_nodeinfo_V2.sh` 의 `verify_hosts` 와 같은 입력 규칙: 공백·쉼표·`|`·탭·줄바꿈 구분, 빈 줄로 입력 종료. 범위식(`node[01-05]`)은 `-w` 와 같은 규칙으로 펼치고, 순서·중복 무시 **완전일치**일 때만 다음 단계. 빈 입력이거나 하나라도 다르면 차이(입력에만 / 대상에만)를 보여주고 즉시 취소, **종료코드 1**.
3. **확인 문구 `y/N` 유지**, 그 단계의 취소 종료코드도 지금처럼 0.
4. **흐름 순서**: 위험어 감지 → 대상 목록 출력 → 호스트 재입력 대조 → `y/N` → 실행.
5. **최적화 기준**: 랩에 가짜 SSH 서버를 여러 대 띄워 최적화 전후 전체 실행시간을 같은 조건으로 3회 이상 측정, 평균 **10% 이상** 단축될 때만 반영. 미만이면 코드 변경은 버리고 측정 결과만 보고.
6. **배포**: `vendor/` 삭제, `gossh_os6`(.60, Go 1.20) + `gossh_os8`(.58) 커밋. `update.sh` 는 빌드하지 않고 바이너리만 강제 복사(DEST1=os6→`gossh`, DEST2=os8→`gossh`, DEST3=os8→`pdsh`). `setup.sh`·README 의 오프라인 빌드 안내는 바이너리 사용으로 변경.
7. **SVG**: `workflow.svg` 1장만, auto_setup `workflow.svg` 와 같은 양식(라이트/다크, 색 범례, 맨 아래 구현 위치).

## 단계별 마일스톤
1. **위험어 판정 개편** — 기존 부분일치 + 새 단어 단위 정규식. 위험/정상 명령 표로 단위 테스트.
2. **호스트 재입력 대조** — 입력 읽기(빈 줄 종료) → 펼치기 → 집합 비교 → 차이 출력/취소. 파이프 입력·불일치·빈 입력·범위식 테스트.
3. **속도 최적화** — 가짜 SSH 서버 벤치(여러 포트) → 병목 측정(pprof) → 후보 적용 → 전후 비교. 10% 미만이면 되돌림.
4. **배포 구조** — vendor 삭제, os6/os8 빌드, update.sh·setup.sh·README 수정.
5. **workflow.svg** — auto_setup 양식으로 재작성(새 확인 단계 포함).
6. **문서·반영** — README/CHANGELOG/PLAN/ARCHITECTURE/WORKFLOW/도움말, master 와 `gossh-standalone` 푸시, .58 미러 동기화.

## 기술 스택 / 아키텍처
- 판정·대조 로직은 `main.go` 에 작은 함수로 추가 — 위험 확인 흐름이 `main.go` 에 있어서.
- 범위식 펼치기는 기존 `expandHostLine` 재사용 — `-w` 와 같은 결과를 보장하려고.
- 랩 빌드는 랩 안의 모듈 캐시/임시 vendor 사본으로 하고 저장소에는 넣지 않는다 — vendor 삭제 후에도 .60(Go 1.20, x/crypto v0.31.0 고정) 빌드를 유지하려고.

## 리스크 및 제약사항
- auto_setup s 메뉴에서 새 위험어 명령(예: `mkfs`, `ifdown`)은 gossh 가 거부해 실행되지 않음 — 사용자 결정으로 수용.
- auto_setup 화면 안내의 `(대상 목록 + y/N)` 은 이제 호스트 재입력 단계가 추가되어 실제 흐름과 다소 다름 — auto_setup 미수정 결정.
- vendor 를 빼면 저장소만으로는 폐쇄망에서 소스 빌드가 불가 — 바이너리 배포로 대체(확정).
- SSH 대기가 대부분이라 최적화 효과가 작을 수 있음 → 10% 기준으로 판단.

## 추천 모델과 예상 토큰 규모 (추정, ±50%)
| 단계 | 추천 모델 | 예상 토큰 |
|---|---|---|
| 1. 위험어 판정 | Sonnet 5.5 | 약 40~60k |
| 2. 호스트 대조 | Sonnet 5.5 | 약 50~80k |
| 3. 속도 최적화 | Opus 5.5 | 약 120~200k |
| 4. 배포 구조 | Sonnet 5.5 | 약 40~70k |
| 5. workflow.svg | Sonnet 5.5 | 약 40~70k |
| 6. 문서·반영 | Sonnet 5.5 | 약 40~60k |
| 합계 | | 약 330~540k |
