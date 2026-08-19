# main_conn-source

vCenter 클러스터에 여러 대의 Bare Metal ESXi 호스트를 **병렬로 등록**하는 도구입니다.
SSL 인증서가 신뢰되지 않은 호스트를 만나면 vCenter가 응답으로 돌려주는 SSL Thumbprint를
자동으로 감지해서 재시도합니다.

> ⚠️ **이 도구는 실제로 vCenter에 ESXi 호스트를 추가(write)합니다.** 대상 호스트 정보
> (IP/계정/비밀번호)와 vCenter 접속 정보가 **소스코드에 하드코딩**되어 있으므로, 반드시
> 실행 전에 `main.go`를 열어 값을 직접 수정한 뒤 다시 빌드해야 합니다. 커맨드라인
> 플래그는 없습니다.

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
git clone <이 저장소 주소> myrepo
cd "myrepo/.claude/VM/legacy-vm-param-fix-external-orchestration/main_conn-source"
bash setup.sh
# 빌드 완료: .../main_conn-source/main_conn
```

## 2. 사용 방법 (실행 전 소스 수정 필수)

이 도구는 커맨드라인 인자를 전혀 받지 않습니다. `main.go` 상단의 아래 값들을 실제
환경에 맞게 직접 수정한 뒤 `bash setup.sh`로 다시 빌드해야 합니다.

```go
vcenterURL := "https://administrator@vsphere.local:YourPassword@vcenter.example.local/sdk"
clusterName := "Production-Cluster"

hosts := []HostTarget{
    {"192.168.10.101", "root", "HostPass1!"},
    {"192.168.10.102", "root", "HostPass2!"},
    {"192.168.10.103", "root", "HostPass3!"},
}
```

- `vcenterURL`: vCenter SDK 접속 URL.
  **주의**: 예시 값 자체가 `id:pw@id2:pw2@host` 형태로 다소 비정상적인 형식입니다.
  `soap.ParseURL`이 파싱 가능한 표준 `https://<vCenter계정>:<비밀번호>@<vcenter주소>/sdk`
  형식으로 반드시 고쳐서 써야 합니다.
- `clusterName`: ESXi 호스트를 등록할 대상 클러스터 이름.
- `hosts`: 등록할 ESXi 호스트 목록. `{IP, root 계정 사용자명, 비밀번호}` 순서로 원하는
  만큼 추가/삭제할 수 있습니다.

수정 후 재빌드하고 그대로 실행하면 됩니다.

```bash
bash setup.sh
./main_conn
```

## 3. 동작 순서

1. `vcenterURL`로 vCenter에 **1회만** 로그인(세션 생성) — 호스트마다 재접속하지 않고
   하나의 세션(`*govmomi.Client`)을 모든 goroutine이 공유합니다.
2. 대상 클러스터(`clusterName`) 조회.
3. `hosts` 목록을 goroutine으로 동시에 처리하되, 세마포어(`chan struct{}`, 버퍼 5)로
   **동시 5대**까지만 병렬로 진행되도록 제한합니다.
4. 각 호스트에 대해 `AddHost_Task` 호출.
   - SSL 미신뢰 오류(`SSLVerifyFault`)가 감지되면, 응답에 포함된 Thumbprint를 자동으로
     추출해 `HostConnectSpec.SslThumbprint`에 주입하고 1회 재시도합니다.
5. 각 호스트별 성공(`[SUCCESS]`)/실패(`[FAIL]`) 메시지를 콘솔에 출력, 전체 goroutine이
   끝날 때까지 대기(`sync.WaitGroup`) 후 종료.

## 4. 알려진 한계

- 커맨드라인 옵션이 없어 대상이 바뀔 때마다 소스 수정 + 재빌드가 필요합니다.
- 비밀번호가 소스코드에 평문으로 남으므로, 실제 값으로 채운 뒤에는 커밋/공유 시
  반드시 값을 지우거나(플레이스홀더로 되돌리거나) 별도 관리 방식으로 옮기는 것을
  권장합니다.
- 동시성(5)도 코드 내 상수로 고정되어 있어 바꾸려면 소스 수정이 필요합니다.
- 전체 타임아웃이 10분(`context.WithTimeout`)으로 고정되어 있습니다.
