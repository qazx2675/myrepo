# tag_setting-source

`ev01`/`ev02`/`ev03`... 이름 규칙으로 생성된 VM에 **사용자 지정 특성(Custom
Attribute)** — `DEPT_NAME`, `PURPOSE`, `VM_TYPE` — 을 순번(ev01, ev02, ...)별로
다른 값으로 **병렬 일괄 설정**하는 도구입니다.

> ⚠️ **이 도구는 실제로 VM의 Custom Attribute 값을 변경(write)합니다.**
> `DEPT_NAME` / `PURPOSE` / `VM_TYPE` 이라는 이름의 Custom Attribute가 vCenter에
> **미리 정의되어 있어야 합니다.** (vSphere Client의 태그 및 사용자 지정 특성 메뉴에서
> 사전 생성 필요 — 정의되어 있지 않으면 `SetCustomValue` 호출이 에러를 반환할 수
> 있습니다.)

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
cd "myrepo/.claude/VM/legacy-vm-param-fix-external-orchestration/tag_setting-source"
bash setup.sh
# 빌드 완료: .../tag_setting-source/tag_setting
```

## 2. 사용 방법

### 준비물

- 환경변수 `VC_PASSWORD`: vCenter 로그인 비밀번호 (필수)
- `hostlist.txt` (기본 파일명, `-hostListFile`로 변경 가능): 대상 물리 호스트(BM)
  이름을 한 줄에 하나씩. `#` 주석/빈 줄 무시, UTF-8 BOM 자동 제거.

### 실행 예시 (호스트 1대당 VM 2대, ev01/ev02 값을 다르게 지정)

```bash
export VC_PASSWORD='실제_비밀번호'

./tag_setting \
  -vcTargetIP=192.168.0.50 \
  -id=administrator@vsphere.local \
  -hostListFile=hostlist.txt \
  -vmCount=2 \
  -deptNames="개발팀,운영팀" \
  -purposes="개발용,운영용" \
  -vmTypes="Linux,Linux"
```

위 예시는 `hostlist.txt`의 각 호스트에 대해 `ev01` VM에는
`DEPT_NAME=개발팀, PURPOSE=개발용, VM_TYPE=Linux`를, `ev02` VM에는
`DEPT_NAME=운영팀, PURPOSE=운영용, VM_TYPE=Linux`를 설정합니다.

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP. |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 ID. |
| `-hostListFile` | `hostlist.txt` | | 대상 물리 호스트(BM) 목록 파일. |
| `-vmCount` | `1` | | 호스트 1대당 대상 VM 수(`ev01`~`ev0N`). 1 이상이어야 함. |
| `-deptNames` | `""` | ✅ | `ev01,ev02,ev03...` 순번별 `DEPT_NAME` 값 (콤마 구분). 최소 `-vmCount`개 필요. |
| `-purposes` | `""` | ✅ | 순번별 `PURPOSE` 값 (콤마 구분). 최소 `-vmCount`개 필요. |
| `-vmTypes` | `""` | ✅ | 순번별 `VM_TYPE` 값 (콤마 구분). 최소 `-vmCount`개 필요. |

`-vmCount=2`인데 `-deptNames`에 값이 1개뿐이면, 실행 즉시(vCenter 접속 전)
`-deptNames 값이 부족합니다...` 에러로 종료합니다. `-vmCount`보다 값이 더 많이
주어지는 것은 허용되며, 초과분(예: ev03용 값)은 무시됩니다.

## 4. 동작 순서

1. 플래그 검증(`-vcTargetIP`, `-vmCount`, 값 개수) → `hostlist.txt`의 호스트 수 ×
   `-vmCount`로 대상 VM 이름 목록(`<호스트>ev01`, `<호스트>ev02`, ...) 생성.
2. `VC_PASSWORD` 로드, vCenter 접속.
3. **데이터센터 지정 없이** `client.ServiceContent.RootFolder`부터 `ContainerView`로
   **vCenter 전체의 VM을 1회 배치 조회**해서 이름→VM 맵으로 인덱싱(여러 데이터센터가
   있어도 "please specify a datacenter" 에러 없이 동작).
4. 대상 VM마다 goroutine을 띄워(별도의 동시성 제한 없음 — "알려진 한계" 참고) 이름으로
   조회:
   - 이름이 매칭되는 VM이 없으면 `VM을 찾을 수 없음 (SKIP)`.
   - 동일 이름의 VM이 2개 이상이면 모호하다고 판단해 `(SKIP)`.
   - 정상 매칭되면 해당 순번(`idx`)의 `DEPT_NAME`/`PURPOSE`/`VM_TYPE` 값을
     `vm.SetCustomValue()`로 3개 각각 설정.
5. 모든 goroutine 완료 후, 실패 목록이 있으면 `[일부 실패]`로 요약 출력, 없으면
   `[성공]` 메시지 출력.

## 6. 디렉토리 구조

```
tag_setting-source/
├── README.md      # 이 문서
├── main.go        # Custom Attribute(DEPT_NAME/PURPOSE/VM_TYPE) 병렬 일괄 설정 로직 전체
├── go.mod / go.sum  # Go 모듈 정의 파일
├── setup.sh       # vendor 패키지로 폐쇄망에서도 빌드하는 스크립트
├── tag_setting    # setup.sh로 빌드된 실행 바이너리
└── vendor/        # 빌드에 필요한 Go 의존성 패키지 모음 (서드파티, 문서화 대상 제외)
```

## 5. 알려진 한계

- 대상 VM마다 **동시성 제한 없이** goroutine을 하나씩 띄웁니다(다른 도구들과 달리
  세마포어가 없음). 대상이 매우 많으면(수백~수천) 동시 API 호출이 급증할 수 있으므로
  대규모 실행 시 주의가 필요합니다.
- `DEPT_NAME`/`PURPOSE`/`VM_TYPE` Custom Attribute가 vCenter에 미리 정의되어 있지
  않으면 개별 VM 단위로 실패가 발생할 수 있습니다(전체 실행은 중단되지 않고 해당
  키만 실패 목록에 기록됨).
- 동일 이름의 VM이 여러 데이터센터/폴더에 중복 존재하면 모호함으로 판단해
  건너뜁니다(어느 것을 바꿀지 자동으로 결정하지 않음).
