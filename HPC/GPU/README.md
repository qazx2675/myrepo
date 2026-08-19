# GPU Power Capping 설정 검증 스크립트

NVIDIA GPU의 Power Limit 75% 설정이 정상적으로 적용되었는지 자동으로 검증하는 스크립트입니다.

---

## 📦 포함 파일

| 파일 | 설명 |
|------|------|
| `check-gpu-power-limit.sh` | GPU Power Limit 설정 검증 스크립트 |
| `fix-gpu-power-limit.sh` | 설정 실패 시 자동 복구 스크립트 |
| `gpu-power-limit.service.example` | systemd service 파일 예시 |
| `gpu-power-limit-check-plan.md` | 검증 계획 및 흐름 |
| `README.md` | 이 파일 |

---

## 🚀 빠른 시작

### 1. 권한 설정
```bash
chmod +x check-gpu-power-limit.sh fix-gpu-power-limit.sh
```

### 2. 검증 실행
```bash
sudo ./check-gpu-power-limit.sh
```

### 3. 수정 필요 시
```bash
sudo ./fix-gpu-power-limit.sh 75  # Power Limit을 75W로 설정
```

---

## 🔍 검증 스크립트 (`check-gpu-power-limit.sh`)

### 사용법

**기본 사용:**
```bash
sudo ./check-gpu-power-limit.sh
```

**특정 GPU만 검증:**
```bash
sudo ./check-gpu-power-limit.sh 0  # GPU 0만 검증
sudo ./check-gpu-power-limit.sh 1  # GPU 1만 검증
```

### 검증 항목

#### 1. 사전 조건 확인
- Root 권한 확인
- nvidia-smi 설치 여부
- nvidia-settings 설치 여부
- systemctl 사용 가능 여부
- GPU 감지 여부

#### 2. Service 파일 검증
- `/etc/systemd/system/gpu-power-limit.service` 파일 존재 여부
- 파일 권한 검증 (644 또는 755)
- Power Limit 설정 명령어 존재 여부
- systemd 파일 형식 검증
  - [Unit] 섹션
  - [Service] 섹션
  - [Install] 섹션
  - ExecStart 필드

#### 3. Service 상태 검증
- Service 활성화 상태 (enabled/disabled)
- Service 실행 상태 (running/stopped)
- 최근 실행 로그 확인

#### 4. GPU Power Limit 실제 적용 검증
- 각 GPU의 현재 Power Limit 확인
- Max Power Limit 확인
- Power Draw 확인
- 설정값과 기대값 비교 (오차범위: ±5%)

#### 5. nvidia-persistenced 상태 확인
- 프로세스 실행 여부
- systemd service 상태 (있는 경우)

### 반환값

| 값 | 의미 |
|----|------|
| 0 | 모든 검증 통과 ✓ |
| 1 | 일부 검증 실패 ⚠ |
| 2 | 심각한 오류 발생 ✗ |

### 출력 예시

```
==============================================================================
  1. 사전 조건 확인
==============================================================================
✓ PASS: root 권한 확인
✓ PASS: nvidia-smi 명령어 존재
ℹ INFO: 버전: NVIDIA-SMI 535.104.05    Driver Version: 535.104.05
✓ PASS: nvidia-settings 명령어 존재
✓ PASS: systemctl 명령어 존재
✓ PASS: GPU 감지: 2개의 GPU 발견

==============================================================================
  2. Service 파일 검증
==============================================================================
✓ PASS: Service 파일 존재: /etc/systemd/system/gpu-power-limit.service
✓ PASS: 파일 권한 정상
ℹ INFO: 파일 권한: 644
ℹ INFO: Service 파일 내용:
---
[Unit]
Description=NVIDIA GPU Power Limit (75W) Persistence
After=nvidia-persistenced.service

[Service]
Type=oneshot
ExecStart=/usr/bin/nvidia-smi -pl 75
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
---
✓ PASS: Power Limit 설정 명령어 발견
ℹ INFO: 설정된 Power Limit: 75W
✓ PASS: ExecStart 필드 존재
✓ PASS: [Unit] 섹션 존재
✓ PASS: [Service] 섹션 존재
✓ PASS: [Install] 섹션 존재

==============================================================================
  3. Service 상태 검증
==============================================================================
✓ PASS: Service 활성화 상태: enabled
✓ PASS: Service 실행 상태: running
...

==============================================================================
  검증 완료 - 종합 보고서
==============================================================================

총 검사 항목: 28
  통과: 25
  실패: 0
  경고: 3

✓ 모든 검증을 통과했습니다!

ℹ INFO: GPU Power Limit 75% 설정이 정상적으로 적용되었습니다.
```

---

## 🔧 수정 스크립트 (`fix-gpu-power-limit.sh`)

### 사용법

**기본 사용 (75W로 설정):**
```bash
sudo ./fix-gpu-power-limit.sh
```

**다른 Power Limit 설정:**
```bash
sudo ./fix-gpu-power-limit.sh 100  # 100W로 설정
sudo ./fix-gpu-power-limit.sh 250  # 250W로 설정
```

### 기능

1. **Service 파일 생성/수정**
   - 자동으로 올바른 형식의 service 파일 생성
   - 기존 파일 백업 (`.backup.{timestamp}`)

2. **Systemd 데몬 재로드**
   - `systemctl daemon-reload` 실행

3. **Service 활성화 및 시작**
   - `systemctl enable` 실행
   - `systemctl start` 실행

4. **적용 확인**
   - 각 GPU의 Power Limit 설정값 검증
   - Max Power Limit 및 Power Draw 표시

5. **Service 상태 확인**
   - `systemctl status` 출력
   - 최근 로그 표시

### 백업 복원

설정 문제 발생 시 다음과 같이 이전 설정으로 복원 가능합니다:

```bash
# 최근 백업 파일 확인
ls -la /etc/systemd/system/gpu-power-limit.service.backup.*

# 백업 복원
sudo cp /etc/systemd/system/gpu-power-limit.service.backup.1692547200 \
         /etc/systemd/system/gpu-power-limit.service

# Systemd 재로드 및 재시작
sudo systemctl daemon-reload
sudo systemctl restart gpu-power-limit.service
```

---

## 📋 Service 파일 형식

### 기본 구조

```ini
[Unit]
Description=NVIDIA GPU Power Limit (75W) Persistence
After=nvidia-persistenced.service
Documentation=https://docs.nvidia.com/deploy/dynamic-power-management/

[Service]
Type=oneshot
ExecStart=/usr/bin/nvidia-smi -pl 75
RemainAfterExit=yes
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

### 설정 항목 설명

| 항목 | 설명 |
|------|------|
| `Description` | Service 설명 |
| `After` | nvidia-persistenced 시작 후 실행 |
| `Type=oneshot` | 일회성 명령어 (시작 후 완료) |
| `ExecStart` | 실행할 명령어 |
| `RemainAfterExit=yes` | 완료 후에도 active 상태 유지 |
| `WantedBy=multi-user.target` | 부팅 시 자동 실행 |

---

## 🛠️ 설정 설치 절차

### 1. Service 파일 생성

```bash
# 방법 1: 스크립트 사용 (자동)
sudo ./fix-gpu-power-limit.sh 75

# 방법 2: 수동 생성
sudo cat > /etc/systemd/system/gpu-power-limit.service << 'EOF'
[Unit]
Description=NVIDIA GPU Power Limit (75W) Persistence
After=nvidia-persistenced.service

[Service]
Type=oneshot
ExecStart=/usr/bin/nvidia-smi -pl 75
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
```

### 2. 권한 설정

```bash
sudo chmod 644 /etc/systemd/system/gpu-power-limit.service
sudo chown root:root /etc/systemd/system/gpu-power-limit.service
```

### 3. Systemd 데몬 재로드

```bash
sudo systemctl daemon-reload
```

### 4. Service 활성화

```bash
sudo systemctl enable --now gpu-power-limit.service
```

### 5. 검증

```bash
sudo ./check-gpu-power-limit.sh
```

---

## 📊 모니터링

### 실시간 모니터링

```bash
# Service 상태 모니터링
sudo systemctl status gpu-power-limit.service

# 로그 실시간 확인
sudo journalctl -u gpu-power-limit.service -f

# GPU Power Limit 실시간 모니터링
watch -n 1 'nvidia-smi --query-gpu=power.limit,power.draw,power.max_limit --format=csv'
```

### 정기적 검증 (Cron)

```bash
# 매일 01:00에 검증 실행 및 로그 기록
echo "0 1 * * * /root/check-gpu-power-limit.sh >> /var/log/gpu-power-limit-check.log 2>&1" | sudo crontab -
```

---

## 🐛 문제 해결

### 문제 1: Service 파일을 찾을 수 없음

**증상:**
```
✗ FAIL: Service 파일이 존재하지 않음
```

**해결방법:**
```bash
# 1. 수정 스크립트 실행
sudo ./fix-gpu-power-limit.sh 75

# 2. 수동 생성
sudo cp gpu-power-limit.service.example /etc/systemd/system/gpu-power-limit.service
sudo systemctl daemon-reload
sudo systemctl enable --now gpu-power-limit.service
```

### 문제 2: Service가 실행되지 않음

**증상:**
```
✗ FAIL: Service 실행 상태: inactive
```

**해결방법:**
```bash
# 1. 에러 메시지 확인
sudo systemctl status gpu-power-limit.service

# 2. 로그 확인
sudo journalctl -u gpu-power-limit.service -n 20

# 3. 서비스 재시작
sudo systemctl restart gpu-power-limit.service

# 4. Service 파일 문법 검사
systemd-analyze verify /etc/systemd/system/gpu-power-limit.service
```

### 문제 3: Power Limit 설정이 적용되지 않음

**증상:**
```
⚠ WARN: GPU 0 Power Limit 설정 불일치
```

**해결방법:**
```bash
# 1. 수동 적용
sudo nvidia-smi -pl 75

# 2. 전체 GPU에 적용
for i in $(seq 0 $(($(nvidia-smi --query-gpu=count --format=csv,noheader | head -n 1) - 1))); do
    echo "GPU $i에 Power Limit 적용..."
    sudo nvidia-smi -i $i -pl 75
done

# 3. Service 재실행
sudo systemctl restart gpu-power-limit.service
```

### 문제 4: nvidia-persistenced가 실행되지 않음

**증상:**
```
⚠ WARN: nvidia-persistenced가 실행 중이지 않습니다
```

**해결방법:**
```bash
# 1. nvidia-persistenced 시작
sudo /usr/bin/nvidia-smi -pm 1  # Persistence mode 활성화

# 2. 프로세스 확인
ps aux | grep nvidia-persistenced

# 3. 강제 시작
sudo /usr/bin/nvidia-persistenced --user=root

# 4. 부팅 시 자동 시작 설정
sudo systemctl enable nvidia-persistenced
```

---

## 📚 추가 명령어

### GPU Power Limit 수동 설정

```bash
# 단일 GPU 설정
sudo nvidia-smi -i 0 -pl 75

# 모든 GPU 설정
sudo nvidia-smi -pm 1  # Persistence mode 활성화
for i in 0 1; do
    sudo nvidia-smi -i $i -pl 75
done
```

### 현재 GPU 설정 조회

```bash
# Power Limit 조회
nvidia-smi --query-gpu=index,power.limit,power.draw,power.max_limit --format=csv

# 상세 정보
nvidia-smi --query-gpu=index,name,power.limit,power.draw,power.max_limit,power.min_limit --format=csv,noheader
```

### GPU 리셋

```bash
# Persistence mode 비활성화
sudo nvidia-smi -pm 0

# GPU 리셋
sudo nvidia-smi -r
```

---

## 🔗 참고 자료

- [NVIDIA Dynamic Power Management](https://docs.nvidia.com/deploy/dynamic-power-management/)
- [nvidia-smi 매뉴얼](https://developer.nvidia.com/nvidia-system-management-interface)
- [Systemd 유닛 파일](https://www.freedesktop.org/software/systemd/man/systemd.unit.html)

---

## 📞 지원

문제가 발생하는 경우:

1. **로그 확인**
   ```bash
   sudo journalctl -u gpu-power-limit.service -n 50
   ```

2. **Service 파일 검증**
   ```bash
   systemd-analyze verify /etc/systemd/system/gpu-power-limit.service
   ```

3. **검증 스크립트 실행**
   ```bash
   sudo ./check-gpu-power-limit.sh
   ```

4. **시스템 정보 수집**
   ```bash
   nvidia-smi
   nvidia-smi -i 0 -pm 0
   systemctl status gpu-power-limit.service
   ```

---

**마지막 업데이트:** 2024년 8월 19일
**버전:** 1.0
**라이선스:** MIT
