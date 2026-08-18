# vc-test-env (vCenter → vcsim 테스트환경 복제 도구) 프로젝트 계획서

## 목표 및 범위

실 vCenter에 접속해서 인벤토리 구조(데이터센터/VM폴더/네트워크폴더/클러스터/호스트/리소스풀/VM)와 각 VM의 설정(CPU/메모리/디스크/NUMA/Affinity/전원정책 등, 현재 다루는 도구들이 실제로 쓰는 필드)을 읽어와 **"레시피"(JSON)** 형태로 저장하고, 이 레시피를 [vcsim](https://github.com/vmware/govmomi/tree/main/vcsim)(govmomi의 vCenter 시뮬레이터) 위에 재생성해서 **실 vCenter와 이름·구조가 동일한 테스트 환경**을 만드는 Go 도구. 실행은 `vc-test-env` 명령 하나로 되고, 재생성된 vcsim에는 govmomi 기반 도구들과 PowerCLI가 **수정 없이 그대로** 접속해서 쓸 수 있어야 함. 레시피는 인터넷이 되는 환경(예: Rocky Linux VM)에서 만든 뒤 폐쇄망(제온 2680 v3/v4, 램 ~300GB, RHEL 8.10, PowerCLI 설치됨) 서버로 옮겨 그 자리에서 다시 빌드할 수 있어야 함.

**포함**: 읽기 전용 인벤토리 추출 → JSON 레시피 저장/캐시 → vcsim에 레시피대로 재생성(실제 이름/IP 그대로) → 트리 형태 출력 → 재검증(diff) → 단일 명령 실행(vCenter 선택/기억) → 폐쇄망 이관.
**제외**: 실 VM의 게스트 OS/디스크 데이터 자체 복제, VM "설정 편집"/호스트 "구성" 탭의 모든 항목(부팅옵션·비디오카드·USB·고급 Advanced System Settings 등) — 현재 필요하지 않은 항목은 만들지 않되, **필드를 나중에 쉽게 추가할 수 있는 구조**로 짠다(아래 "확장 가능한 필드 구조" 참고). `-pwsh` 자동접속 기능은 불필요 판단으로 제외 — `Connect-VIServer`로 사용자가 직접 붙으면 됨.

## 확장 가능한 필드 구조 (핵심 설계)

`internal/fields/fields.go`에 **필드 레지스트리**를 둔다. 각 필드는 `{Key, Extract 함수, Apply 함수}` 하나로 정의되고, 새 필드가 필요해지면 이 슬라이스에 항목 하나만 추가하면 `extract`/`build`/`diff` 전부에 자동으로 반영된다 — `inventory`/`builder`/`recipe` 쪽 코드는 손댈 필요 없음.

```go
type Field struct {
    Key     string
    Extract func(vm mo.VirtualMachine) (value string, present bool)
    Apply   func(spec *types.VirtualMachineConfigSpec, value string)
}
var VMFields = []Field{ /* 지금 필요한 것만 */ }
```

## 현재 포함하는 필드 (1차 범위)

| 필드 | 대상 | 비고 |
|---|---|---|
| CPU 코어/소켓 수, vCPU 수, 메모리MB | VM | 구조적 필수값, ConfigSpec 표준 필드 |
| `sched.mem.lpage.enable1GPage` | VM | ExtraConfig |
| `sched.mem.prealloc*` | VM | ExtraConfig |
| `sched.swap.vmxSwapEnabled` | VM | ExtraConfig |
| `numa.vcpu.maxPerVirtualNode` | VM | ExtraConfig |
| CPU Affinity(AffinitySet) | VM | ConfigInfo.CpuAffinity (구조화 필드) |
| 호스트 전원정책 | Host | ConfigManager.PowerSystem |
| 디스크 용량, 네트워크 어댑터-포트그룹 연결 | VM | 트리 출력/재현용 구조값 |
| 데이터센터/VM폴더/네트워크폴더/클러스터/호스트 이름·트리 | 구조 | 이미 순회 대상이라 추가 비용 거의 없음 |

**주의**: 위 ExtraConfig 키 이름은 vmx 어드밴스드 옵션 관례를 따른 것으로, 실제 vCenter(192.168.0.50)에서 `extract`를 처음 돌려서 값이 정확히 이 키로 잡히는지 검증이 필요하다(마일스톤 0). 다르면 `fields.go`의 `Key` 문자열만 고치면 된다.

## 사용성 / CLI 설계

### 인증
- 실 vCenter 접속: `VC_USER`/`VC_PASS` 환경변수 (기존 `vm-param-check`와 동일 변수명). 비밀번호는 디스크에 저장 안 함.
- vcsim은 기본적으로 아무 자격증명이나 받으므로, 재생성된 vcsim에 접속하는 다른 도구들(Go든 PowerCLI든)은 손댈 필요 없음.

### 실행 흐름
```
vc-test-env [-vc=<target>] [-refresh]
  │
  ├─ 히스토리(~/.vc-test-env/history.json) 확인
  │   ├─ 0개  → "접속할 vCenter 주소를 입력하세요:" 프롬프트
  │   ├─ 1개  → 자동 선택
  │   └─ 2개+ → 번호로 선택 (+"새로 추가")
  │   (-vc 주어지면 스킵)
  │
  ├─ 캐시된 recipe.json 재사용 (-refresh 시 강제 재추출)
  │   없으면 VC_USER/VC_PASS로 extract → 캐시 저장 → 히스토리 등록
  │
  ├─ vcsim 기동 + recipe로 build
  ├─ tree 자동 출력
  ├─ 접속 정보 출력 (vcsim 주소 + govmomi 도구용 예시 + Connect-VIServer 예시)
  └─ 포그라운드 유지, Ctrl+C 종료
```

## 서브커맨드

- `vc-test-env` (인자 없음) — 위 실행 흐름(기본 동작)
- `vc-test-env extract -vc=<ip>` — 레시피만 추출/갱신
- `vc-test-env tree -vc=<ip>` — 대상(실 vCenter/vcsim 어느 쪽이든) 구조를 트리로 출력만
- `vc-test-env diff -vc=<ip>` — 재생성된 vcsim을 다시 추출해서 원본 레시피와 비교

## 단계별 마일스톤

0. **실현 가능성 검증(최우선)** — 192.168.0.50에서 위 필드들이 vcsim에 재현 가능한 형태로 존재하는지 확인.
1. `extract` 구현 (읽기 전용).
2. `tree` 구현 — 원본 vCenter로 먼저 검증.
3. `build` 구현 — 레시피 → vcsim 재생성.
4. 히스토리/캐시 + 단일 명령 조립.
5. 재생성본 `tree` 재실행으로 육안 비교.
6. `diff` 구현.
7. 기존 Go 도구 + PowerCLI 스모크테스트.
8. 폐쇄망(RHEL 8.10) 이관 문서화.

## 기술 스택 / 아키텍처

```
vc-test-env/
├── go.mod
├── main.go                   # 서브커맨드 라우팅, 기본 동작 = up 플로우
├── PLAN.md
├── README.md
├── internal/
│   ├── connect/connect.go     # govmomi 클라이언트 연결 (VC_USER/VC_PASS)
│   ├── fields/fields.go        # 확장 가능한 필드 레지스트리 (핵심)
│   ├── inventory/walk.go        # DC/폴더/클러스터/호스트/VM/네트워크 순회 (extract/tree 공유)
│   ├── recipe/recipe.go          # JSON 레시피 스키마 + 읽기/쓰기
│   ├── builder/build.go           # vcsim 기동 + 레시피 재생성
│   ├── tree/render.go              # ASCII 트리 렌더링
│   └── history/history.go          # ~/.vc-test-env/history.json, recipes/ 캐시
└── vendor/                          # 오프라인 빌드용 (go mod vendor로 생성, Go 있는 곳에서)
```

## 리스크 및 제약사항

- **vcsim 필드 재현 가능 여부가 핵심 리스크** — 마일스톤 0에서 반드시 확인.
- **ExtraConfig 키 이름 검증 필요** — 위 "주의" 참고.
- **govmomi/vcsim 버전 고정 필요** — `go.mod`/`vendor`를 레시피와 함께 이관.
- **recipe.json은 내부 인프라 정보를 담음** — git에 커밋하지 않고 `.gitignore` 처리.
- **PowerCLI-vcsim 호환 범위**는 실제로 붙여보고(`Connect-VIServer -Server <vcsim주소> -Force`) 확인 필요, 안 되는 항목은 README에 기록.
- **검증 완료** — Rocky Linux(192.168.0.58)에서 실 vCenter(192.168.0.50)를 대상으로 extract/tree/build/diff/PowerCLI 접속까지 전부 실행 확인. 남은 한계는 README "알려진 한계" 참고(씨드 호스트가 트리에 하나 더 보이는 것, 다중 데이터센터 미지원, 전원정책 미적용 등).

## 저장소

애초 clipSend(Android 프로젝트) 저장소와는 무관해서 로컬(`C:\Users\qazx2\AndroidStudioProjects\vc-test-env`)에 별도 저장소로 만들었다가, `vm-param-check`/`vm-param-fix` 등 다른 vCenter 도구들이 이미 모여 있는 `myrepo`의 `.claude/vcenter-test-env-vcsim/`로 옮겨서 같이 관리함. (이후 `.claude/VM 업무/vcenter-test-env-vcsim/`로 한 번 더 정리됨)
