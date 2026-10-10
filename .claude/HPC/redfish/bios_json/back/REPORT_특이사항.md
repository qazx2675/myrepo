# Redfish BIOS 조사 특이사항 리포트

대상: HPE / Dell / Lenovo / Cisco / Supermicro / NVIDIA / SSONIC 총 53개 모델 (문서 기반 조사, 실제 장비 접속 없음).
방법: 공식 문서에서 증거 수집 → Jev API 로 URI·프로토콜 정합성 판정(기준 0.8) → 독립 출처 2차 검증 → 실패 시 재조사 → 재실패 시 Claude+Jev 합동.
세부 근거는 각 `<벤더>/<그룹>/README.md`, 벤더별 `SUMMARY*.md` 참조.

## 1. 한눈에 보기

| 벤더 | 프로토콜 | 전체 BIOS 속성 확보 | 주요 문제 |
|---|---|---|---|
| HPE Gen9/G8 | json (iLO 4 2.30+) | 235개 (iLO4 문서 합집합) | Bios URI 미검증, DL560 G8 BIOS 속성 제공 여부 미확인 |
| HPE Gen10 | json | 401개 (Intel 샘플, 모델 전용 아님) | 모델별 레지스트리 없음 |
| HPE Gen11 | json | **실패** | 레지스트리가 HPE Support Center 로그인·EULA 뒤에 있음 |
| HPE XD220V | json | **실패** | BMC 가 iLO 가 아닌 "HPE Cray XD BMC", URI 미검증 |
| Dell 14G | json | 965개 (합집합) | AMD 전용 속성 혼입 가능 |
| Dell 15G | json | 686개 (R650 대리 샘플, 노출 440) | R750 계열 자체 레지스트리 없음 |
| Dell 16G 이후 | json | 1431개 (합집합) | Intel/AMD 속성 혼재, 일부 값 잘림 |
| Lenovo | json | **실패** | 공개 문서에 속성 전체 없음 |
| Cisco | **xml** (UCSM) | 토큰 91개 (일부) | M4 토큰 표 없음, X210c M7 표 잘림 |
| Supermicro | json | **실패** | 가이드에 예시 2개뿐 |
| NVIDIA DGX A100 | json | **실패** | BIOS 경로 문서 없음 |
| SSONIC | unknown | **실패** | 공개 자료 전무 |

## 2. 수집 실패 항목

### 2.1 BIOS 속성 전체 목록 자체를 못 구한 모델
| 모델 | 사유 | 해결 방법 |
|---|---|---|
| HPE DL360 G11, DL380 G11 | 모델 전용 레지스트리(U54)는 HPE Support Center 다운로드(EULA 동의 필요). 공개 문서는 iLO 6 AMD 샘플(540개)뿐, 공개 Intel 샘플(401개)은 iLO 5 와 동일해 제외 | 아래 두 zip 을 받아 `BiosAttributeRegistryU54` 파싱, 또는 실제 iLO 에서 `/redfish/v1/Registries` 조회 |
| HPE DL560 G11 | 동일 (ROM 패밀리 U59, 별도 패키지) | 동일 |
| HPE XD220V | 모델 전용 문서 없음, BMC 가 Cray XD BMC | 실장비 덤프 |
| Lenovo 전 모델 (SR630, SR650, SD530, SR630 V2, SR645 V3, SR675 V3, SD650 V3) | Lenovo 가 속성 전체를 공개 문서에 싣지 않음. 레지스트리는 BMC 에서만 제공 | 실제 XCC 에서 `/redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` 조회 |
| Supermicro AS-1115HS-TNR | 가이드에 예시 속성 2개뿐 | 실장비 덤프 |
| NVIDIA DGX A100 | 공개 자료에 속성·BIOS 경로 없음 | 실장비 덤프 |
| SSONIC 11개 전 모델 | 영/한 검색에서 벤더·모델 자료 없음. 메인보드/BMC 제조사도 불명 (추측 안 함) | 실장비 또는 제조사 문의 |

HPE Gen11 Support Center 링크 (v3.00_08-20-2026, 에이전트 조사 기준 최신):
- DL360/DL380/ML350 Gen11 (U54): https://support.hpe.com/km/software/MTX-e269fb8869a04cb2
- DL560 Gen11 (U59): https://support.hpe.com/km/software/MTX-14a1b1423ce3475a

### 2.2 속성은 있으나 모델 전용이 아닌 경우 (정확도 주의)
- **HPE Gen10 (DL360/DL560/DL580/XL270d G10, DL360 G10 Plus):** HPE 공개 Intel 샘플 401개를 공통 사용. 모델별 차이 미반영. 기본값 전부 null.
- **HPE Gen9 (DL360/XL170r/XL250/XL270d G9):** iLO 4 데이터 모델 문서 "HpBios Attributes" 절의 합집합 235개. 요약형 fetch 도구로 추출해 누락 가능. ROM 계열별 레지스트리(P89, U14, U25 등) 미확보, XL250/XL270d 의 ROM 매핑 불확실.
- **Dell 14G (R640/R740/R840/C6420/DSS8440):** iDRAC9 Attribute Registry PDF(4.40 이하) 기준 965개, 14G 전 플랫폼 합집합. AMD 전용이 섞였을 수 있음. 기본값 null, 값 형식 불명 속성은 type `unknown`.
- **Dell 15G (R750/R750xa/R750xs):** 같은 15G Intel 인 **R650 의 실제 레지스트리**(iDRAC 7.10.30.00, BIOS 1.12.1)를 대리 사용. 686개 중 실제 노출 440개. 슬롯·GPU 관련 속성은 모델별로 다를 수 있음.
- **Dell 16G 이후 (R760XA/XE9680/R6615/XE9780/R660/R860/C6620):** 1431개 합집합(Intel+AMD 혼재, R6615 는 AMD). `DcuIpPrefetcher`, `SetupPassword` 값이 잘려 unknown, 그 다음 속성(…Password Status)은 이름이 잘려 제외. 10만 자 청크 요약 읽기라 전사 오류 가능.
- **Cisco:** 토큰명은 Cisco 가이드의 UI 이름이고 XML 속성명(`vpCPUPerformance` 등)과 매핑 못 함. cisco.com 직접 다운로드가 403 이라 WebFetch 요약 기반이며 원문 대조 안 됨.

### 2.3 URI / 프로토콜 미검증 (`unverified`)
| 대상 | 내용 |
|---|---|
| HPE Gen9 (iLO 4) | `/redfish/v1/Systems/1/Bios`, `.../Bios/Settings` Jev 0.69, 0.67 (재조사 후에도 미달). Redfish 서비스 자체는 verified |
| HPE DL560 G8 | Redfish 서비스는 0.94, BIOS 속성 제공 여부는 제3자 위키만 근거(0.92) → HPE 문서 확인 필요 |
| HPE XD220V | Redfish 지원은 제품 페이지 근거 0.99, Bios URI 미확인 |
| Dell XE9780 (iDRAC10) | json 프로토콜 0.99, Bios URI 미확인 |
| Dell 16G+ | `/Bios/BiosRegistry` 출처 1곳뿐(0.92) |
| NVIDIA DGX A100 | Bios URI 미확인. `/redfish/v1/Systems/Self/Bios` 는 0.00 으로 기각 |
| Supermicro | `Bios.ResetBios`, `Bios.ChangePassword` 액션 경로(출처 1곳), 가이드의 레지스트리 JSON 경로(일반 예시) |
| Cisco B200/B480 M5 | XML 객체 DN(`biosVProfile`, `biosSettings`)은 ucsmsdk 소스 기준(0.88), 독립 Cisco 문서 없음 |
| Cisco X210c M7 | Redfish BIOS URI unknown (UCSM=XML, Intersight 관리 모드=Intersight REST JSON, Redfish 아님) |
| Lenovo SR630 V2 | XCC 세대 불명, 1세대 계열로 편입한 것은 약한 추정 |

## 3. 펌웨어 버전에 따라 설정값/동작이 다른 항목

| 모델/그룹 | 내용 |
|---|---|
| HPE Gen9 (iLO 4) | 펌웨어 2.00~2.29 는 `/rest/v1` 만 제공, **2.30 이상부터 Redfish 준수**. 속성 단위 차이는 미확인 |
| HPE Gen10 (iLO 5) | 문서 v2.73~v3.09 구간 속성 **이름 집합은 동일**(값 변경은 구분 못 함). ROM 별 차이 확인불가 |
| HPE Gen10 Plus / Gen11 | `BaseConfigs` 등 OEM 링크가 `Bios/Oem/Hpe/` 아래로 이동 (G10 대비 구조 차이) |
| HPE Gen10 최신 ROM | U45 3.30_07-31-2024 확인, U32 는 비공식 글의 3.60만, U54·U59 는 2023년 버전까지만(Gen11 Support Center 기준 3.00_08-20-2026 은 에이전트 조사값), U34·U46 미확인 |
| Dell 14G | iDRAC9 4.40 에서 `SysPrepClean`, 4.30.30.30 에서 `AgesaVersion` **추가**. 삭제·이름변경 여부는 모름 |
| Dell 15G | 구버전 레지스트리 못 구해 확인불가 |
| Dell 16G 이후 | iDRAC9 7.xx 와 iDRAC10 1.30.xx 가이드가 거의 동일(734,988자 vs 734,710자, 비교 구간 일치, 278자 차이 위치 불명) |
| Supermicro | 최신 BIOS 4.0 / BMC 01.13.11. 구버전 차이 미확인. 대기 설정 URI 는 `Bios/Settings` 가 아니라 **`Bios/SD`** |
| NVIDIA DGX A100 | 최신 BMC 00.25.02 / SBIOS 1.33. SBIOS 0.30 에서 **기본값 변경**, 1.18 에서 **메뉴 항목 제거** |
| Cisco B200/B480 M5 | 3.2(1)~4.1(3h) 사이 토큰 차이를 `old_fw_diff.md` 에 정리. 4.3/6.0 가이드의 M5 행은 읽지 못해 4.1(3h) 이후 변경 불명 |
| Lenovo | 최신 펌웨어·구버전 차이 모두 unknown |

벤더 간 URI 차이(참고): HPE `Systems/1`, Dell `Systems/System.Embedded.1`, Lenovo `Systems/1` (`/Bios/Pending`, `/Bios/Settings` 없음), Supermicro `Systems/1` (`/Bios/SD`).

## 4. 동일 JSON 으로 묶은 그룹과 신뢰도

| 그룹 | 근거 | 신뢰도 |
|---|---|---|
| HPE DL360 G9 / XL170R G9 / XL250 G9 / XL270d G9 | 동일 iLO 4 문서 | 중 (ROM 매핑 불확실) |
| HPE DL560 G10 + DL580 G10 | ROM 패밀리 (U34/U54 공유 확인) | 중 |
| HPE DL360 G11 + DL380 G11 | 동일 U54 패키지 | 높음 |
| HPE DL560 G11 (별도) | 별도 U59 패키지 (Jev 1.0) | 높음 |
| Dell R640+R740 / R750+R750xa | 모델별 가이드 차이 정도 | 중 |
| Dell 16G 이후 7개 모델 | 동일 문서 항목 참조 | 낮음~중 (실제 노출 속성은 모델별로 다름, 특히 AMD R6615) |
| Lenovo SR630+SR650+SD530 / SR645 V3+SR675 V3 | XCC 세대·플랫폼 | 낮음 (잠정) |
| Cisco B200 M5 + B480 M5 | 동일 UCSM BIOS 토큰 | 중 |

## 5. 권장 후속 조치
1. 가능한 장비에서 `/redfish/v1/Systems/<id>/Bios` 와 `/redfish/v1/Registries/<레지스트리>` 를 직접 덤프해 이 폴더의 합집합·대리 샘플을 실측값으로 교체 (특히 Lenovo, Supermicro, NVIDIA, SSONIC, HPE Gen11).
2. HPE Gen11 은 위 Support Center zip 2개를 내려받으면 `BiosAttributeRegistryU54`/`U59` 를 바로 파싱 가능 (다운로드·EULA 동의는 사용자가 직접).
3. `unverified` URI(2.3 표)는 실장비 응답으로 확정.
4. Dell 15G/16G+ 는 모델별 노출 속성을 실측과 대조.
5. SSONIC 은 제조사에 메인보드/BMC 정보를 문의한 뒤 재조사.
