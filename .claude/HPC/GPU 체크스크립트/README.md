# GPU Power Limit 점검 스크립트 (check-gpu-power-limit.sh)

`nvidia-persistenced` + `gpu-power-limit.service`로 설정한 GPU power capping(최대 전력의 75%)이
정상적으로 적용되어 있는지 일회성으로 점검하는 스크립트입니다.

`gossh` 등으로 여러 서버에 배포한 뒤 일괄 실행하는 용도이므로, 출력은 한 줄에 하나씩
`OK\t설명` / `FAIL\t설명` 형식으로 나옵니다 (`grep FAIL`로 문제 서버만 바로 필터링 가능).

## 전제: gpu-power-limit.service 구성

```
# /etc/systemd/system/gpu-power-limit.service
[Unit]
Description=Set NVIDIA GPU Power Limit
After=multi-user.target nvidia-persistenced.service

[Service]
Type=oneshot
ExecStart=스크립트경로/set-gpu-power-limit.sh

[Install]
WantedBy=multi-user.target
```

`set-gpu-power-limit.sh`는 `nvidia-smi -pl <목표W>`로 GPU 최대 전력의 75%를 설정합니다.

## 1. 사전 준비 (필수 수정 항목)

스크립트 상단의 변수를 실제 환경에 맞게 수정해야 합니다.

| 변수 | 설명 |
|---|---|
| `TARGET_POWER_LIMIT_W` | 목표 power limit(W). GPU 최대 전력 × 0.75 값을 직접 계산해서 입력 |
| `SERVICE_NAME` | systemd service 이름 (기본값 `gpu-power-limit`) |
| `SERVICE_FILE` | service 유닛 파일 경로 (기본값 `/etc/systemd/system/${SERVICE_NAME}.service`) |
| `SET_SCRIPT_PATH` | `set-gpu-power-limit.sh`의 절대경로. 비워두면 이 항목 점검은 건너뜀 |

## 2. 사용 방법

```bash
chmod +x check-gpu-power-limit.sh
./check-gpu-power-limit.sh
```

nfs 계정 등에 업로드해두고 `gossh`로 여러 서버에 일괄 실행하는 방식으로 사용합니다.

## 3. 점검 항목

1. service 유닛 파일(`/etc/systemd/system/gpu-power-limit.service`) 존재 여부
2. (선택) `ExecStart=`가 지정한 `SET_SCRIPT_PATH`를 가리키는지
3. service `active` 상태
4. service `enabled` 상태 (재부팅 후에도 유지되는지)
5. `nvidia-smi` 설치 여부
6. GPU별 실제 power limit이 `TARGET_POWER_LIMIT_W`와 일치하는지 (다중 GPU 전부 검사)

## 4. 출력 예시

```
OK	service file exists (/etc/systemd/system/gpu-power-limit.service)
OK	service is active (gpu-power-limit)
OK	service is enabled (gpu-power-limit)
OK	GPU 0 power limit = 825W (target 825W)
FAIL	GPU 1 power limit = 800W (target 825W)
```

⚠️ **주의사항**: 참고용 점검 스크립트입니다. `nvidia-smi` 출력 형식은 드라이버 버전에 따라
달라질 수 있으므로, 대규모 배포 전에는 일부 서버에서 결과를 직접 확인하는 것을 권장합니다.

## 5. 디렉토리 구조

```
GPU 체크스크립트/
├── README.md                    # 이 문서
└── check-gpu-power-limit.sh     # GPU power limit 적용 상태를 점검하는 스크립트 본체
```
