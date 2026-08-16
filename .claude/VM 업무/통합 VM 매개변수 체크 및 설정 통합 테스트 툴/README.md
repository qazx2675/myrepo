# 통합 VM 매개변수 체크 및 설정 통합 테스트 툴

`vm-param-check`(체크+자동교정, `-fix` 내장)와 `vc-test-env`(vcsim 테스트환경 복제)를
**한 폴더로 묶어**, 인터넷이 안 되는 폐쇄망 서버로 그대로 옮겨서 빌드→테스트까지 끝낼 수 있게
만든 배포 패키지입니다. 두 도구 모두 Go 모듈 자체에 `vendor/`를 포함하고 있어 `go build -mod=vendor`
한 줄이면 인터넷 없이 빌드됩니다.

```
통합 VM 매개변수 체크 및 설정 통합 테스트 툴/
├── vm-param-check/   체크+자동교정 도구 (소스+vendor/, 빌드하면 이 폴더 안에 바이너리 생성)
├── vc-test-env/      실 vCenter -> vcsim 복제 도구 (소스+vendor/, 빌드하면 이 폴더 안에 바이너리 생성)
├── testkit/          위 두 도구를 엮어서 실행하는 셸 스크립트 4개 (build-all/start-vcsim/run-tests/stop-vcsim)
└── README.md         이 문서
```

각 도구 자체의 전체 옵션/동작 설명은 `vm-param-check/README.md`, `vc-test-env/README.md`를
참고하세요. 이 문서는 **두 도구를 묶어서 어떻게 빌드하고 테스트하는지**에 집중합니다.

## 1. 폐쇄망으로 이관하기 (최초 1회, 인터넷 되는 곳에서)

이 폴더를 통째로 압축해서 옮기면 됩니다. `vendor/`가 각 도구 안에 이미 포함되어 있어서
부분 복사 없이 폴더 전체를 옮기는 게 핵심입니다 — 일부만 옮기면 빌드가 실패합니다.

```bash
# 인터넷 되는 곳(예: 이 저장소를 git clone한 서버)에서
cd myrepo
tar czf vm-param-check-testkit.tar.gz \
  ".claude/VM 업무/통합 VM 매개변수 체크 및 설정 통합 테스트 툴"

# USB, scp, 사내 파일서버 등으로 폐쇄망 서버에 복사
scp vm-param-check-testkit.tar.gz <폐쇄망서버>:/tmp/
```

## 2. 폐쇄망 서버에서 압축 해제 + 초기 설정

```bash
cd /tmp
tar xzf vm-param-check-testkit.tar.gz
cd ".claude/VM 업무/통합 VM 매개변수 체크 및 설정 통합 테스트 툴"

# 원하는 최종 위치로 옮기고 싶다면 (예: /opt/vm-param-check-testkit)
# mv "$(pwd)" /opt/vm-param-check-testkit && cd /opt/vm-param-check-testkit
```

**필요 환경 (폐쇄망 서버 기준)**:
- Go 1.21 이상 (Rocky Linux + Go 1.26.5 기준으로 빌드·테스트 검증 완료)
- 인터넷 **불필요** — `vendor/`에 `github.com/vmware/govmomi` 등 의존성이 전부 포함됨
- vcsim으로만 테스트할 경우 실 vCenter 접속 불필요. 단, **최초 1회** 실제 vCenter 구조를
  복제하려면(레시피 추출) 그 vCenter에 접속 가능해야 함 — 그 이후로는 캐시된 레시피
  (`~/.vc-test-env/recipes/*.json`)만으로 vcsim을 계속 재기동할 수 있어 실접속이 불필요해짐.
  이 폴더를 이관할 때 `~/.vc-test-env/recipes/`도 함께 챙겨서 옮기면 최초 1회 접속조차 건너뛸 수
  있습니다(3장 참고).

```bash
chmod +x testkit/*.sh
```

## 3. (선택) 레시피 캐시 미리 챙기기

실 vCenter 구조를 미리 추출해둔 레시피가 있으면, 폐쇄망 서버에서 vCenter 접속 없이 바로 vcsim을
띄울 수 있습니다. 인터넷 되는 곳(원 vCenter에 접속 가능한 곳)에서 미리 추출해서 같이 옮기세요.

```bash
# 인터넷 되는 곳에서, 원본 vCenter 구조를 레시피로 추출
cd vc-test-env
go build -mod=vendor -o vc-test-env .
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./vc-test-env extract -vc=192.168.0.50

# 추출된 레시피 파일 확인 (기본 경로: ~/.vc-test-env/recipes/)
ls ~/.vc-test-env/recipes/
# -> 192.168.0.50.json

# 이 파일을 폐쇄망 서버의 같은 경로로 복사
scp ~/.vc-test-env/recipes/192.168.0.50.json <폐쇄망서버>:~/.vc-test-env/recipes/
```

레시피만 있으면 vcsim 기동 시 실 vCenter 접속을 완전히 건너뛰고 캐시를 그대로 재생합니다
(`testkit/start-vcsim.sh` 참고).

## 4. 빌드

```bash
./testkit/build-all.sh
```

`vm-param-check/vm-param-check`, `vc-test-env/vc-test-env` 두 바이너리가 생성되면 성공입니다.
내부적으로 `go build -mod=vendor`를 쓰므로 인터넷 연결을 시도하지 않습니다.

## 5. vcsim 기동 (테스트용 가상 vCenter)

```bash
# 캐시된 레시피가 있으면 그걸 그대로 사용 (실 vCenter 접속 없음)
./testkit/start-vcsim.sh -vc=192.168.0.50

# 레시피가 아예 없어서 최초로 추출부터 해야 하는 경우 (실 vCenter 접속 필요)
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./testkit/start-vcsim.sh -vc=192.168.0.50
```

`127.0.0.1:54321`(고정 포트)에서 응답할 때까지 자동으로 대기했다가 "기동 완료"를 출력합니다.
백그라운드로 뜨고, PID는 `testkit/out/vcsim.pid`에 저장됩니다.

## 6. 체크 + 자동교정 테스트 실행

```bash
export VC_USER=user      # vcsim은 계정/비밀번호 값 자체를 검사하지 않음 — 아무 문자열이나 가능
export VC_PASS=pass
./testkit/run-tests.sh
```

3가지를 자동으로 검증합니다:
1. **체크만**: vcsim 대상으로 `vm-param-check` 실행, CSV 생성까지 하드에러 없이 끝나는지
2. **체크 + 자동교정(`-fix`)**: 같은 대상에 `-fix`를 추가로 줘서 게이트 검증 → dry-run →
   (`y` 자동응답) → 실제 적용 → 재검증까지 파이프라인 전체가 하드에러 없이 끝나는지.
   외부 도구(affinity_setting 등) 없이 `vm-param-check` 바이너리 하나로 전부 처리됩니다.
3. **`[미지원]` 태그 검증**: vcsim이 자체 구현하지 않는 3개 필드
   (`config.memoryReservationLockedToMax`, `config.numaInfo.coresPerNumaNode`,
   `cpuid.coresPerSocket`)가 `[설정없음]`이 아니라 `[미지원]`으로 정확히 표시되는지 — vcsim
   대상일 때만 이 판정이 적용되고, 실제 vCenter 대상일 때는 개입하지 않습니다
   (`vm-param-check/README.md`의 "`미지원`이 붙는 필드" 절 참고).

결과 CSV/로그는 전부 `testkit/out/`에 남습니다. 재실행할 때마다 덮어써지므로, 이력을 남기고
싶으면 그때그때 `testkit/out/`을 별도로 백업하세요.

## 7. vcsim 종료

```bash
./testkit/stop-vcsim.sh
```

## 8. 실제 vCenter를 직접 대상으로 체크/교정하고 싶을 때

`testkit/`은 vcsim(가상환경) 검증용입니다. 실제 vCenter를 직접 체크/교정하려면 vcsim을 거치지
않고 `vm-param-check` 바이너리를 바로 실행하면 됩니다 — 사용법은 `vm-param-check/README.md`
참고. 이때는 vcsim에서만 나타나는 `[미지원]` 태그가 나타나지 않고, 항상 실측값 기반으로
`OK`/`FAIL`/`설정없음`이 판정됩니다.

```bash
cd vm-param-check
export VC_USER=administrator@vsphere.local
export VC_PASS='<비밀번호>'
echo "192.168.0.50" > vcenter.txt
./vm-param-check -vcenterList=vcenter.txt \
  -ht=on -cores=1 -numa=2 -cpu=2 -mem=4 -disk=40 -shares-ev01=1000 \
  -out=result.csv
# 문제 없으면 -fix를 추가해서 자동교정까지: -out=result.csv -fix
```

## 9. 자주 겪는 문제

| 증상 | 원인/해결 |
|---|---|
| `go build` 시 네트워크 요청 시도 | `-mod=vendor`를 빼먹은 것. `testkit/build-all.sh`는 항상 붙여서 실행하므로, 직접 `go build`를 칠 때만 조심 |
| `run-tests.sh`가 "127.0.0.1:54321(vcsim)이 응답하지 않습니다"로 즉시 실패 | `start-vcsim.sh`를 먼저 실행하지 않았거나, 이전 vcsim이 비정상 종료된 상태. `testkit/out/vcsim.log` 확인 |
| `start-vcsim.sh`가 "이미 실행 중인 vcsim이 있습니다"로 거부 | 이전 테스트의 vcsim이 아직 떠 있음. `./testkit/stop-vcsim.sh`로 정리 후 재시도 |
| 레시피 추출 단계에서 실 vCenter 접속 실패 | `VC_USER`/`VC_PASS` 오타 확인 (예: `vsphere.local`을 `vshpere.local`로 잘못 치는 경우가 흔함), 방화벽/네트워크 경로 확인 |
| vcsim 대상인데 `[미지원]` 태그가 하나도 안 보임 | `vm-param-check` 바이너리가 오래된 버전일 수 있음. `testkit/build-all.sh`를 다시 실행해서 재빌드 |
