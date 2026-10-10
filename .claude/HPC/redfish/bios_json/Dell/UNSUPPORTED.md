# Dell — Redfish BIOS 미지원 / 미확보 범위

## 1. 관리망 버전 자체가 Redfish BIOS 를 지원하지 않는 경우

| 관리망 / 펌웨어 | Redfish BIOS | 대체 수단 | 해당 사용자 모델 | 근거 |
|---|---|---|---|---|
| iDRAC7/iDRAC8 < 2.30.30.30 | Redfish 없음 → **xml** (WS-MAN `DCIM_BIOSService`) / racadm | WS-MAN, racadm | 없음 | iDRAC8 2.30.119.30 Release Notes ("Added support for Redfish 1.0") |
| iDRAC7/iDRAC8 2.30 ~ 2.41 | Redfish 는 있으나 **BIOS 리소스 없음** (2.40.40.40 이상은 Redfish SCP 로 BIOS 내보내기·가져오기 가능) | SCP(`EID_674_Manager.*`), WS-MAN(xml), racadm | 없음 | 2.40.40.40 Release Notes. FC630(2.41.40.40)·C6320(2.40.40.40) 실캡처에 Bios 링크 없음 |
| iDRAC7/iDRAC8 ≥ 2.50.50.50 | 지원 | — | 없음 | 2.50.50.50 Release Notes·API Guide |
| iDRAC9 (모든 버전) | 지원 | — | R640, R740, R840, C6420, DSS8440, R750, R750xa, R750xs, R760XA, XE9680, R6615, R660, R860, C6620 | iDRAC9 API Guide 4.20 + 3.15~7.20 실캡처 |
| iDRAC10 | 지원 (SCP 액션 이름만 `OemManager.*` 로 바뀜) | — | XE9780 | Dell iDRAC10 목업 3종 |

→ **사용자 보유 Dell 모델 중 Redfish BIOS 를 쓸 수 없는 모델은 없다.** iDRAC8 이하 장비는 보유하지 않았다.

## 2. 지원은 되지만 모델별 원본 레지스트리를 공개 자료로 확보하지 못한 모델

이 모델들은 같은 세대의 다른 모델 데이터를 대리로 쓴다(unverified). 정확한 목록은 실장비에서 `GET /redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` 와 `GET .../Bios` 로 받아야 한다.

| 모델 | 관리망 | 대리 데이터 | 우려 사항 |
|---|---|---|---|
| R640, R740 | iDRAC9 (14G) | R740xd 7.00.00.182 / BIOS 2.24.0 | 슬롯·드라이브베이 수 차이 |
| R840 | iDRAC9 (14G, 4S) | 〃 | 4소켓(UPI, Proc3/Proc4 항목), 별도 BIOS 라인 |
| C6420 | iDRAC9 (14G 멀티노드) | 〃 | 별도 BIOS 라인 |
| DSS8440 | iDRAC9 (14G GPU) | 〃 | 별도 BIOS 라인. 공개 자료가 가장 적음 |
| R750xa | iDRAC9 (15G) | R750 7.10.30 | GPU 슬롯 구성 |
| R750xs | iDRAC9 (15G) | R750 7.10.30 | xs 계열 별도 BIOS |
| XE9680, R660, R860, C6620 | iDRAC9 (16G Intel) | R760xa/R760 7.10.50, R660xs 7.20.10.05 | XE9680 GPU 슬롯, R860 4S, C6620 멀티노드 |
| R6615 | iDRAC9 (16G AMD) | R7615 7.00.60.00 `/Bios` (이름·값만) | **type·허용값 없음**(BiosRegistry 미확보) |
| XE9780 | iDRAC10 | R770 1.10.17.00 (Xeon 6 E-core) | P-core(하이퍼스레딩), HGX B300 베이스보드 System, 슬롯 구성 |

## 3. 공통으로 얻을 수 없는 값

- **default**: Dell BiosAttributeRegistry 에는 DefaultValue 가 없다(모든 캡처에서 확인). 13G 의 HelpText "Default: X" 14건만 예외다. 공장 기본값이 필요하면 실장비에서 `Bios.ResetBios` → 재부팅 → `GET /Bios` 로 얻는다.
- **read_only 의 고정값**: Dell 레지스트리의 ReadOnly/Hidden/Immutable 은 현재 설정(SysProfile 등)에 따라 바뀌는 상태값이라 "항상 읽기전용"인지 판단할 수 없다. Dependencies 배열을 따로 해석해야 한다.
