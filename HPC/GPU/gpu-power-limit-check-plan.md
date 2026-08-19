# GPU Power Capping 설정 검증 계획서

## 📋 목표
NVIDIA nvidia-persistenced를 통한 Power Capping 75% 설정이 정상적으로 적용되었는지 자동 검증

---

## 🔍 검증 항목

### 1. Systemd Service 파일 검증
- [ ] `/etc/systemd/system/gpu-power-limit.service` 파일 존재 확인
- [ ] 파일 권한 확인 (644 권장)
- [ ] 파일 내용 검증 (PowerLimit 설정값 확인)
- [ ] 파일 포맷 검증 (올바른 systemd 형식)

### 2. Systemd Service 상태 검증
- [ ] Service 활성화 상태 확인 (`systemctl is-enabled`)
- [ ] Service 실행 상태 확인 (`systemctl is-active`)
- [ ] Service 시작 로그 확인 (`journalctl`)
- [ ] Service 재시작 정책 확인

### 3. GPU Power Limit 실제 적용 검증
- [ ] `nvidia-smi` 명령어로 각 GPU의 Power Limit 확인
- [ ] 현재 설정값이 75%와 일치하는지 확인
- [ ] 모든 GPU에 동일하게 적용되었는지 확인
- [ ] Power Limit이 persistent 상태인지 확인

### 4. nvidia-persistenced 상태 검증
- [ ] nvidia-persistenced daemon 실행 여부 확인
- [ ] nvidia-persistenced 프로세스 상태 (`ps aux`)
- [ ] nvidia-persistenced 로그 확인

### 5. 시스템 재부팅 후 적용 검증 (선택)
- [ ] 재부팅 후 설정 유지 여부 확인
- [ ] 부팅 로그에서 설정 적용 확인

---

## 📊 검증 흐름

```
1. 사전 조건 확인
   ├─ nvidia-smi 존재 확인
   ├─ root 권한 확인
   └─ systemctl 명령어 사용 가능 확인

2. Service 파일 검증
   ├─ 파일 존재 여부
   ├─ 파일 내용 파싱
   └─ PowerLimit 값 추출

3. Service 상태 검증
   ├─ 활성화/비활성화 상태
   ├─ 실행/중지 상태
   └─ 최근 로그 확인

4. GPU 실제 설정값 검증
   ├─ 각 GPU의 Power Limit 확인
   └─ 기대값(75%)과 비교

5. 최종 보고서 생성
   ├─ 통과/실패 항목 정리
   └─ 필요 조치 사항 제시
```

---

## 🔧 스크립트 요구사항

### 입력
- 선택: GPU 인덱스 (기본: 모든 GPU)
- 선택: 상세 로그 레벨 (기본: 정상)

### 출력
- 각 검증 항목별 통과/실패 상태
- 현재 설정값
- 기대값
- 필요한 수정 사항
- 색상 코딩 (녹색: 통과, 빨강: 실패, 노랑: 경고)

### 예외 처리
- nvidia-smi 없을 경우
- GPU 미인식 시
- Service 파일 손상 시
- 권한 부족 시

---

## 📝 반환값
- 0: 모든 검증 통과
- 1: 일부 검증 실패
- 2: 심각한 오류 발생
