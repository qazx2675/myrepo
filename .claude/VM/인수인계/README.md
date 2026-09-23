# VM 자동화 도구 인수인계 문서

> 이 폴더 하나만 읽으면 `.claude/VM/` 아래 모든 도구를 빌드·실행·수정할 수 있도록 만든 문서 세트입니다.
> **vSphere(가상화)를 처음 접하는 분**을 기준으로 썼습니다. 리눅스 SSH 접속과 파일 편집은 할 줄 안다고 가정합니다.

---

## 처음 오셨다면 이 순서로 읽으세요

| 순서 | 문서 | 무엇을 알 수 있나 | 예상 시간 |
|---|---|---|---|
| 1 | [00_시작하기](./00_시작하기.md) | 이 도구들이 왜 존재하는지, 첫날 뭘 해야 하는지 | 10분 |
| 2 | [01_기초지식](./01_기초지식.md) | vCenter/ESXi/VM/NUMA/lpage/affinity가 각각 뭔지 | 30분 |
| 3 | [02_공통_실행환경](./02_공통_실행환경.md) | Go 설치, 폐쇄망 빌드, 비밀번호 넘기는 법, 공통 입력파일 | 30분 |
| 4 | 아래 "도구별 문서" 중 **당장 쓸 것 하나** | 실제 실행 방법 | 20분 |
| 5 | [30_유지보수_AI_활용가이드](./30_유지보수_AI_활용가이드.md) | 코드를 고쳐야 할 때 AI에게 어떻게 시키는지 | 15분 |

바쁘면 **00 → 02 → 쓸 도구 문서** 순서만 봐도 실행은 됩니다. 다만 01을 건너뛰면 옵션의 의미를 이해하기 어렵습니다.

---

## 도구별 문서

### 자주 쓰는 것 (인수 후 실제 운영에 사용)

| 문서 | 폴더 | 한 줄 설명 | 위험도 |
|---|---|---|---|
| [11. vm-param-check](./11_vm-param-check-usability-improvement.md) | `vm-param-check-usability-improvement/` | **VM 설정이 기준에 맞는지 점검하고, 원하면 그 자리에서 자동 교정.** 이 저장소의 주력 도구 | 🔴 설정변경 |
| [14. vm-network-migration](./14_vm-network-migration.md) | `vm-network-migration/` | VM 네트워크 포트그룹 일괄 이관 + 실패 시 자동 롤백 | 🔴 설정변경 |
| [12. vm_verifier](./12_vm_verifier.md) | `vm_verifier/` | VM 생성 직후 MAC 대조로 교차설치(역설치) 탐지 | 🟢 읽기전용 |
| [13. lpage_search](./13_lpage_search.md) | `lpage_search/` | Large Page 메모리 사이징 계산기 (접속 없이 계산만) | 🟢 읽기전용 |
| [10. VM_setup](./10_VM_setup.md) | `VM_setup/` | affinity/lpage/태그/vSwitch/라이선스/VM생성 등 개별 설정 도구 모음 | 🔴 설정변경 |
| [15. esxi-log-check](./15_esxi-log-check.md) | `esxi-log-check/` | ESXi 치명적 로그(MCE/PSOD/APD 등) 수집·분석 + 웹 대시보드 | 🟢 읽기전용 |

### 테스트·검증용 (실제 인프라를 건드리지 않음)

| 문서 | 폴더 | 한 줄 설명 | 위험도 |
|---|---|---|---|
| [18. vcenter-test-env-vcsim](./18_vcenter-test-env-vcsim.md) | `vcenter-test-env-vcsim/` | 실 vCenter 구조를 복제해 가짜 vCenter(vcsim)를 띄움 | 🟡 테스트용 |
| [19. integrated-vm-param-check-test-tool](./19_integrated-vm-param-check-test-tool.md) | `integrated-vm-param-check-test-tool/` | 위 둘을 묶은 폐쇄망 반출용 테스트 패키지 | 🟡 테스트용 |
| [20. gemini_vcsim-pipeline-test](./20_gemini_vcsim-pipeline-test.md) | `gemini_vcsim-pipeline-test/` | 생성→설정→점검→교정 전 과정 통합 테스트(go test) | 🟡 테스트용 |

### 참고/레거시 (새로 시작할 땐 쓰지 않음)

| 문서 | 폴더 | 한 줄 설명 | 위험도 |
|---|---|---|---|
| [16. vm-param-setting-check](./16_vm-param-setting-check.md) | `vm-param-setting-check/` | 체크 전용 구버전. 11번 도구로 대체됨 | 🟢 읽기전용 |
| [17. vm-setting-go-lang](./17_vm-setting-go-lang.md) | `vm-setting-go-lang/` | worklist 기반 설정/생성/호스트등록 도구 4종 | 🔴 설정변경 |
| [21. powershell](./21_powershell.md) | `powershell/` | 폐쇄망에 PowerShell + PowerCLI 설치 스크립트 | 🟡 설치용 |

---

## 유지보수 (코드를 고쳐야 할 때)

| 문서 | 내용 |
|---|---|
| [30_유지보수_AI_활용가이드](./30_유지보수_AI_활용가이드.md) | 이 도구들은 AI로 만들었습니다. 수정도 AI에게 시키는 것이 표준 절차입니다. 그 방법 |
| [31_변경요청서_양식](./31_변경요청서_양식.md) | **복사해서 채워 넣는 양식.** 이 양식대로 요청하면 결과 품질이 일정합니다 |

---

## 찾아보기

| 문서 | 내용 |
|---|---|
| [40_폴더구조](./40_폴더구조.md) | 전체 폴더 트리와 폴더 간 관계, 어느 파일을 고쳐야 하는지 |
| [90_용어집](./90_용어집.md) | vSphere 용어와 이 저장소 고유 용어(ev01, BM, worklist 등) |
| [91_트러블슈팅_FAQ](./91_트러블슈팅_FAQ.md) | 자주 나는 오류 메시지와 원인 |
| [99_인수인계_체크리스트](./99_인수인계_체크리스트.md) | 인수 완료 판정 기준. 하나씩 체크하세요 |
| [계획서](./계획서.md) | 이 문서 세트를 왜 이렇게 구성했는지 |

---

## 배포용 단일 HTML

`VM_인수인계_핸드북.html` 파일 하나에 이 폴더의 모든 문서가 목차와 함께 들어 있습니다.
**인터넷도 서버도 필요 없습니다** — 파일을 복사해서 브라우저로 열기만 하면 됩니다. 폐쇄망 반출이나 문서 전달에는 이 파일을 쓰세요.

문서(.md)를 고친 뒤에는 다시 만들어야 합니다:

```bash
cd .claude/VM/인수인계
python3 build_handbook.py
```

---

## 이 문서 세트의 원칙

- **원본은 `.md`입니다.** HTML은 항상 md에서 생성됩니다. HTML을 직접 고치지 마세요 — 다음 생성 때 사라집니다.
- **도구 폴더의 `README.md`가 1차 자료입니다.** 이 문서는 그것을 초보자 기준으로 다시 쓰고 서로 엮은 것입니다. 옵션 하나의 정확한 동작이 의심스러우면 해당 폴더의 README와 `main.go`의 `flag.` 정의를 확인하세요.
- **모든 도구는 폐쇄망에서 빌드됩니다.** 의존성이 `vendor/` 폴더에 통째로 들어 있어서 인터넷이 필요 없습니다. 이 성질을 깨는 수정(새 외부 라이브러리 추가)은 하지 마세요.

---

## ⚠️ 문서 작성 중 발견한 소스–README 불일치

이 문서를 만들면서 각 도구의 `main.go` `flag` 정의와 폴더 README를 대조한 결과, **원본 README가 소스와 다른 곳**을 찾았습니다. 인수 직후 헤맬 수 있는 지점이라 미리 알려둡니다.

| 도구 | README의 서술 | 실제 소스 | 상세 |
|---|---|---|---|
| `esxi-log-check` | `-server <주소:포트>` 옵션으로 웹 대시보드 제공 | **`-server` 플래그가 없고 HTTP 서버 코드도 없음.** 스크립트가 이 옵션을 넘기므로 웹 서버 메뉴는 실패할 가능성이 높음 | [15번 5절](./15_esxi-log-check.md) |
| `vm_lpage_bulk` | `-concurrency` 기본 20 | **해당 플래그 없음.** ev03 그룹도 없음 | [17번 10절](./17_vm-setting-go-lang.md) |
| `vm_create` (vm-setting-go-lang) | 예시에 `-folderName`, Share에 `nomal` | **`-folderName` 없음**(vm_connect 전용), Share는 정수만 | [17번 10절](./17_vm-setting-go-lang.md) |

이 문서들의 옵션표는 **소스 기준으로 바로잡아** 적어두었습니다. 원본 README까지 고치려면 [31_변경요청서_양식](./31_변경요청서_양식.md)을 쓰세요.
