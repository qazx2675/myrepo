# power_setting-source

BM(ESXi 호스트)의 **전원 관리 정책을 "고성능(High Performance)"** 으로 바꾸는 도구입니다.
VM 이 아니라 **호스트** 설정입니다(vSphere Client: 호스트 > 구성 > 하드웨어 > 전원 관리 > 정책 편집 > 고성능).

⚠️ **주의사항 (Disclaimer)**
설정 변경 스크립트이므로, 실행 후 무작위로 서버 몇 대를 골라 실제로 "고성능"으로 바뀌었는지 직접 확인하세요.

## 1. 빌드 방법

```bash
cd V2/VMsetup/power_setting-source
bash setup.sh          # 또는 V2 폴더에서: bash setup.sh power_setting
# 빌드 완료: .../power_setting-source/power_setting
```

`setup.sh`가 V2 공유 의존성(`../../govendor/govmomi-0.55.1-standard`)을 `vendor`로 링크하므로 폐쇄망에서도 빌드됩니다.

## 2. 사용 방법

필요한 것은 **vCenter 주소**와 **작업 대상 파일(BM 목록)** 두 가지뿐입니다.

```bash
export VC_PASSWORD='비밀번호'
cd V2/VMsetup
./power_setting-source/power_setting -vcTargetIP=192.168.0.50 -worklistFile=lsh.txt
```

- 작업 대상 파일은 `vm_setup.sh` 가 쓰는 `<user>.txt` 를 그대로 쓸 수 있습니다(한 줄에 BM 하나, 첫 칸만 사용, `#` 주석·빈 줄 무시).
- **도메인 없이 hostname 만 적어도 됩니다.** `esxi-node-001` 로 적으면 vCenter 에 `esxi-node-001.domain` 으로 등록된 호스트를 찾습니다
  (정확한 이름 → 짧은 이름(첫 `.` 앞) 비교 순. 짧은 이름이 같은 호스트가 2대 이상이면 에러로 알려 줍니다 — `vswitch_setting` 과 같은 규칙).
- 이미 고성능인 호스트는 건너뜁니다(다시 실행해도 안전).
- 실패가 1건이라도 있으면 종료코드 1.

## 3. 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 주소 |
| `-worklistFile` | `worklist.txt` | | BM 목록 파일 (현재 폴더 기준) |
| `-id` | `lscsystems@vsphere.local` | | vCenter 계정 |
| `-concurrency` | `20` | | 동시에 처리할 호스트 수 |

## 4. 동작 순서

1. BM 목록 읽기(중복 제거).
2. vCenter 접속 → `ContainerView` 로 **모든 호스트의 이름·전원 정보를 호출 1번으로** 수집.
3. 이름 맵(정확한 이름 / 짧은 이름)으로 BM 을 찾음.
4. 호스트가 알려 주는 정책 목록(`powerSystemCapability.availablePolicy`)에서 shortName `static`(고성능)의 key 를 찾음(없으면 1).
5. 현재 정책이 이미 그 key 면 스킵, 아니면 `HostPowerSystem.ConfigurePowerPolicy` 호출 — 호스트끼리 병렬(`-concurrency`).
6. `성공 / 스킵 / 실패` 요약 출력.
