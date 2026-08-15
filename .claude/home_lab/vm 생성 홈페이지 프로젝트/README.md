# VM 자동생성 웹 포털 (home_lab)

Rocky Linux VM(예: 192.168.0.58) 위에서 동작하는, govmomi 기반 vCenter/ESXi 자동화 CLI 스크립트들을
RBAC + 암호화된 자격증명 + 감사 로그를 갖춘 웹 포털로 감싼 프로젝트.

## 구성

- `src/` — Go 백엔드 전체 소스 (cmd/, internal/, go.mod, go.sum). 데이터베이스(SQLite)와 마스터키,
  바이너리 산출물은 시크릿/환경 종속이라 포함하지 않음.
- `docs/session_summary.html` — 이 프로젝트를 만들며 나눈 Q&A, 아키텍처, 사용한 데몬/프로그램,
  발생했던 버그와 해결 과정을 정리한 문서.
- `docs/os_deploy_plan.md` — 다음 단계로 계획 중인 "OS 자동 배포(M10~M13)" 기능 계획서
  (골든 이미지 Clone 방식, 하드웨어 스펙 검토 포함).
- `deploy/vm-portal.service` — 재부팅 후에도 자동 기동되도록 등록하는 systemd 유닛 파일.

## 주요 기능

- Phase 1~9: 호스트 등록, vSwitch/포트그룹 구성, VM 생성, MAC/IP 추출, NUMA/large page 튜닝,
  전원 제어, 파괴적 작업(vm_delete 등)의 dry-run 미리보기, CSV 리포트 조회/다운로드까지 파이프라인화.
- RBAC: 사용자 레벨별 접근 제어, 파괴적 작업은 상위 권한 + 확인 문구 입력 필요.
- 자격증명 암호화 저장 (기본: 파일 기반 마스터키 / 선택: HashiCorp Vault KV v2 백엔드).
- 모든 주요 작업에 대한 감사 로그(`/audit` 페이지에서 조회).
- 클라우드 콘솔 스타일 UI (사이드바+탑바, 상태 배지, SVG 아이콘).

## 빌드 & 배포 (Rocky Linux 기준)

```bash
cd src
go build -o ../bin/server ./cmd/server
```

환경변수:

| 변수 | 설명 | 기본값 |
|---|---|---|
| `VMPORTAL_REPORTS_DIR` | Phase 9 CSV 리포트 저장 경로 | `<base>/data/reports` |
| `VMPORTAL_SECRET_BACKEND` | `file` 또는 `vault` | `file` |
| `VAULT_ADDR`, `VAULT_TOKEN` | Vault 백엔드 사용 시 | - |
| `VMPORTAL_VAULT_SECRET_PATH` | Vault KV v2 시크릿 경로 | `secret/data/vm-portal/master-key` |
| `VMPORTAL_VAULT_KEY_FIELD` | Vault 시크릿 내 키 필드명 | `key` |

systemd 상시 서비스 등록:

```bash
sudo cp deploy/vm-portal.service /etc/systemd/system/vm-portal.service
sudo systemctl daemon-reload
sudo systemctl enable --now vm-portal
```

> SELinux Enforcing 환경이면 바이너리에 `bin_t` 컨텍스트를 지정해야 함
> (`chcon -t bin_t bin/server` + `semanage fcontext -a -t bin_t '/root/vm/portal/bin(/.*)?'` + `restorecon -Rv bin/`).
> 자세한 원인과 진단 과정은 `docs/session_summary.html` 참고.

## 자세한 배경

전체 설계 배경, 겪었던 문제(SELinux 203/EXEC, PID 혼동 등), 질의응답 히스토리는
`docs/session_summary.html`을 열어서 확인.
