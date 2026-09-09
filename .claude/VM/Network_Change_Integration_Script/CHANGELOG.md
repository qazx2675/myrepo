# CHANGELOG — Network_Change_Integration_Script

날짜순(최신이 위).

## 2026-09-09 — 최초 구현 (nci-v0.1.0)

### 신규

- **`change.sh`** — 통합 오케스트레이터. 전처리 → IP 변경 → LDAP → 결과 집계 →
  포트그룹 질의. 서브커맨드: `port` / `rollback` / `--retry ip|ldap` /
  `--debug-inventory`. 단계 격리: `--only` / `--from`. 디버그: `-d1`~`-d3`.
- **에러코드 체계 A~G** (§10). 화면 복사가 안 되는 환경을 전제로 코드만 크게
  출력하고, 원인·대처는 `README.md` 대응표에.
- **전처리** (`lib/preprocess.sh`, §4) — `vswitch_<계정>.txt` 를 Case 1/2/3 규칙으로
  정확히 3열(`BM.도메인 PG VLAN`)로 표준화. IP 앞 3옥텟 + `-0`. 도메인 기본값
  `seccae.com`. 이미 FQDN 인 줄과 IP 를 BM 으로 쓰는 줄은 멱등 처리.
- **2패스 타임아웃** (§8.2) — 1차 짧게 → 타임아웃 호스트 중 ping 되는 것만 2차
  길게. ping 안 되면 즉시 실패 확정.
- **OS 6 분기** (§8.1) — `os6_hostgroup` 의 호스트는 별도 gossh/바이너리로 나눠
  호출. OS6/일반 혼재 시 그룹별 2회.
- **결과 리다이렉션** (§6) — `on_off` / `ip_ok` / `ldap_ok` / `on_off.failed` /
  `failed`(에러코드 포함) 5종. 기존 파일은 `.bak.<시점>` 백업 후 재작성.
  포트그룹 대상은 `ip_ok` 만 (IP 실패 VM 의 VLAN 고립 방지, §5.2).
- **인시던트** (`lib/incident.sh`, §5.3) — 포트그룹 보류 시 저장, `change.sh port
  <인시던트>` 로 재개. `backup_index.txt` 에 세 프로젝트 백업 위치를 인덱싱만
  (자체 백업 안 만듦, §7.1).
- **롤백** (§7.2) — `change.sh rollback <인시던트>` 가 역순(포트그룹 → LDAP → IP).
  포트그룹은 `nm run.sh --rollback`, LDAP 은 `ldap-config-engine -rollback` 자동.
  IP 는 자동 롤백 없어 대상 목록 출력 + 수동 안내.
- **중앙 conf 렌더링** (§2.2) — `integration.conf` → `conf/ip_change.conf`.
  ldap/nm 은 플래그로 전달.
- **`setup.sh`** — 폐쇄망 오프라인 빌드. 3개 프로젝트를 각자 위치에서 빌드하고
  엔진 바이너리를 `bin/` 으로 수집.

### 하위 프로젝트 변경 (원본에도 반영, 계획서 §1.2 예외)

- `ldap_setting`: 자산현황 중복 → 마지막 줄 우선 / `default_site`
  (conf 키 + `-default-site` 플래그) / `ev01~03` 접미사 fallback / 롤백 시
  미매칭 호스트 유지.
- `vm-network-migration`: `LoadVMList` 첫 필드만 읽음 / 호스트 이름 FQDN↔short
  매칭 / `nm-inventory`(`run.sh --debug-inventory`) 추가 / "찾을 수 없음" 오류에
  후보 목록·색인 개수 표시.

### 알려진 제약

- `ip-change-engine` 은 dry-run 을 지원하지 않아 `--dry-run` 의 IP 단계는
  대상 표시만 하고 엔진을 실행하지 않습니다.
- IP 변경은 자동 롤백이 없습니다.
- 엔진 stdout 파싱에 의존합니다 (계약은 CI 로 감시).
- 계획서 §16 미해결: `on_off` 성공 기준(IP·LDAP 둘 다), 짧은 이름 DNS 해석
  전제, 상위폴더 다중 환경 근본 원인.
