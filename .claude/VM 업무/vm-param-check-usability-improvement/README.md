# VM 매개변수 체크/설정 통합 툴 — 사용성 및 성능 개선 프로젝트

`vm-param-check`를 현업에서 쓸 때 겪던 두 가지 문제 — ① 매번 옵션을 손으로 다 입력해야 하는 번거로움, ② 대상 VM이 몇 대뿐이어도 초기 조회가 느린 문제 — 를 해결한 개선 프로젝트입니다. 실제 도구(소스코드)는 이 폴더 바로 아래 [`vm-param-check/`](./vm-param-check/)에 그대로 있습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정을 변경한 후 반드시 랜덤하게 몇 개의 서버를 직접 접속·확인하여 실제로 설정이 제대로 반영되었는지 교차 검증을 진행하십시오.

## 빠른 시작 (지금 바로 실행해보기)

```bash
# 1) 저장소를 받고 실제 도구 폴더로 이동 (인터넷 필요, 딱 이번 한 번만)
git clone https://github.com/qazx2675/myrepo.git myrepo
cd "myrepo/.claude/VM 업무/vm-param-check-usability-improvement/vm-param-check"

# 2) 빌드 (인터넷 불필요 — vendor/에 의존성이 전부 포함되어 있음)
bash setup.sh

# 3) 실제 인프라 없이 동작만 먼저 확인 (아무것도 안 건드림)
./vm-param-check -demo
```

여기까지 되면 빌드는 끝났습니다. 이제 실제 vCenter를 체크하려면:

```bash
# 4) vCenter 인증 정보
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'

# 5) 체크할 vCenter 주소 목록 (한 줄에 하나)
echo '192.168.0.50' > vcenter.txt

# 6) 체크할 VM hostname 목록 (한 줄에 하나)
printf '192ev01\n192ev02\n' > targets.txt

# 7) 옵션을 하나도 안 주고, 폴더명으로 자동매칭해서 체크
#    (-specRoot 아래에 스펙이 미리 준비되어 있어야 함 — 없으면 8번으로)
./vm-param-check -vcenterList=vcenter.txt -f=targets.txt -specRoot=./SPEC_DIR -out=result.csv
```

**스펙이 아직 없다면** (`-specRoot` 아래 디렉터리가 비어있다면) 먼저 만들어야 합니다 — VM이 속한 vCenter 폴더 이름(예: `TST-CAE001-SAMP48c-QRST`)과 똑같은 이름으로:

```bash
# 8) 스펙 디렉터리+틀 생성 (vCenter 연결 안 함)
mkdir -p ./SPEC_DIR
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE001-SAMP48c-QRST"

# 9) 생성된 ./SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt 파일을
#    열어서 ht/cores/numa/cpu/mem/disk/shares-ev01 값을 채운 뒤, 7번 명령을 다시 실행
```

### 보조 스크립트로 한 번에 하기

위 4~9번을 매번 손으로 치는 대신, 이 폴더(도구 폴더)에 있는 보조 스크립트 두 개를 쓸 수 있습니다.

- **`folder_setup.sh`** — 위 8~9번을 대신합니다. `SPEC_DIR`(스크립트와 같은 위치에 만들어지는 로컬 폴더) 아래에 새 스펙을 만들 때, 폴더 이름과 필수 값(ht/cores/numa/cpu/mem/disk/shares-ev01)을 대화형으로 물어보고, 필요하면 ev02/ev03 값도 이어서 물어봅니다(비워두면 스킵). 폴더명 규칙 검사와 중복 거부는 `vm-param-check -initFolder`가 그대로 해줍니다.
  ```bash
  bash folder_setup.sh
  ```
  이렇게 만든 스펙은 팀이 함께 쓰도록 **git에 커밋하는 것을 권장**합니다(인증정보 같은 민감정보가 아니라 CAE 스펙 정의값이라서).

- **`vm_setting_check_insert.sh`** — 위 4~7번을 대신합니다. 스크립트 상단의 `VC_USER`/`VC_PASS`/`VCENTER_LIST`/`SPEC_ROOT` 변수와, `set_user()` 함수 안의 `user` 값(예: `user="kdh"`)을 채워두면, `-f "<user>.txt"`로 대상 목록을 읽고 `result_<user>.csv`로 결과를 저장합니다. 실행하면 "실제로 설정을 변경(-fix)하시겠습니까?"를 먼저 물어보고, `y`를 답해도 `vm-param-check` 자체의 최종 변경 확인을 한 번 더 거칩니다(이중 확인).
  ```bash
  bash vm_setting_check_insert.sh
  ```

옵션을 매번 직접 지정하고 싶다면(스펙 자동매칭 없이) 이렇게도 됩니다:

```bash
./vm-param-check -vcenterList=vcenter.txt -f=targets.txt \
  --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 --shares-ev01=2000 \
  --out=result.csv
```

**폐쇄망(오프라인) 서버에 옮겨서 쓰려면** 1~2번 대신, 인터넷 되는 곳에서 이 폴더 전체를 압축해 USB/scp로 옮긴 뒤 그 서버에서 `bash setup.sh`만 실행하면 됩니다 — 더 자세한 절차와 각 옵션의 의미는 아래 "사용법" 절의 하위 문서를 참고하세요.

## 이 프로젝트에서 무엇이 바뀌었나

| 문제 | 해결 |
|---|---|
| 체크할 때마다 `-cpu`/`-cores`/`-numa`/`-mem`/`-disk`/`-shares-ev01`/`-ht`를 손으로 입력해야 함 | VM이 속한 vCenter 인벤토리 폴더 이름(예: `TST-CAE001-SAMP48c-QRST`)만으로 스펙 파일을 자동으로 찾아 옵션을 채우는 **`-specRoot` 폴더명 기반 스펙 자동매칭** 추가 |
| 신규 스펙을 만들 때마다 디렉터리·spec.txt를 손으로 작성 | **`-initFolder`/`-template`**로 스캐폴드 자동 생성(기존 스펙 복사 가능) |
| VM이 정식 CAE 폴더 규칙을 안 따르는 `Task`(임시) 폴더에 있으면 스펙을 못 찾음 | 포트그룹 이름에서 원래 폴더명을 유추 → 실패 시 대화형으로 직접 입력받는 **Task 폴더 예외 처리** 추가 |
| `-f`로 대상 VM 2대만 지정해도 인벤토리 전체 크기에 비례해서 느려짐 | 대상 이름만 가볍게 먼저 조회한 뒤, 그 VM들에 대해서만 무거운 속성을 조회하도록 **2단계 조회로 전환** — 3,000대 인벤토리 기준 실측 3.22초 → 0.20초 |
| 대상을 여러 vCenter에 나눠 지정했을 때 일부를 못 찾아도 조용히 넘어감 | 요청한 대상 중 못 찾은 게 있거나 vCenter 접속이 실패하면 **반드시 경고**하도록 추가(실행 초반·마지막 양쪽에 표시) |
| `-yes`가 실제 설정 변경 확인까지 건너뜀 | `-yes`는 스펙 자동매칭 확인만 생략하고, **`-fix`의 실제 변경 확인은 항상 물어보도록** 변경 |

## 사용법

전부 하위 [`vm-param-check/README.md`](./vm-param-check/README.md)에 있습니다. 특히 처음 `-specRoot`를 써보신다면 그 문서의 **"2-4. 폴더명 기반 스펙 자동매칭"** 절이 처음부터 끝까지 예시 명령어로 따라할 수 있게 구성되어 있습니다.

빌드·설치(폐쇄망 오프라인 빌드 절차 포함)도 같은 문서의 "1. 빌드 및 설치 방법"을 참고하세요 — `vendor/`에 의존성이 전부 포함되어 있어 이번 개선으로 새로 추가된 의존성 없이 그대로 오프라인 빌드됩니다.

## 설계 배경과 검증 근거

이 개선을 진행하며 확인한 사실(코드 근거), 결정 사항, vcsim/실 vCenter 검증 결과는 [`계획서.md`](./계획서.md)에 단계별로 정리되어 있습니다. 특히 "속도 문제"의 실제 원인이 어디였는지(0장), 폴더명 매칭 규칙이 실제 예시로 어떻게 검증됐는지(3단계), 다중 vCenter/`-yes` 관련 결정 배경(3-1·3-2단계) 등은 이 문서에서만 확인할 수 있습니다.

## 기존 개별 도구와의 관계

저장소의 `vm-param-setting-check/`(체크 전용, 구세대)와 `legacy-vm-param-fix-external-orchestration/`는 삭제하지 않고 그대로 남겨뒀습니다. 이 프로젝트는 하위의 통합 도구(`vm-param-check`) 위에 사용성 개선을 얹은 것으로, 새로 시작하는 경우 이 폴더 아래의 도구만 쓰면 됩니다.
