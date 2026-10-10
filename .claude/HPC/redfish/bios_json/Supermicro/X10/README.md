# Supermicro X10 (BMC 펌웨어 3.xx)

- 프로토콜 판정(BIOS): **none** (Redfish 자체는 있으나 Bios 리소스가 없음). BIOS 설정 자동화는 SUM 의 XML(`GetCurrentBiosCfg`/`ChangeBiosCfg`)만 가능 -> 이 저장소 도구(Redfish JSON)로는 **지원 불가**.
- 사용자 보유 모델: 없음 (X10 계열 보유 목록에 없음).
- 속성: **unavailable** (`bios_attributes.json` 은 메타만, `attributes: []`).
- 최신 펌웨어(다운로드 피드 2026-07-19, X10DRi/-T): BMC 3.91, BIOS 3.4a. https://www.supermicro.com/en/support/resources/downloadcenter/firmware/MBD-X10DRI/BMC

## 근거 (verified = 공식 문서 2건 이상)
| 항목 | 내용 | 출처 | 상태 |
|---|---|---|---|
| Redfish 지원 | "X10/X11 플랫폼에 각각 3.xx / 1.xx BMC 펌웨어로 Redfish 제공" | Redfish Reference Guide Rev 2.0a (2019-02-07) 1장 https://update.shared.it/SUPERMICRO/X10DRT-P/IPMI/Redfish_Ref_Guide_2.0a.pdf (Supermicro 원문 PDF 미러) ; Redfish User Guide Rev 6.1 "Supermicro enables Redfish feature sets on Intel-based X10 and AMD-based H11 and later" https://www.supermicro.com/manuals/other/RedfishUserGuide.pdf | verified |
| Bios 리소스 | Reference Guide 2.0a 의 URI 표에서 `/Systems/1/Bios`, `Bios/SD`, `Bios.ResetBios`, `Bios.ChangePassword` 모두 "only X11DP supports" -> X10 에는 없음 | Guide 2.0a 2.3 List of Available APIs | verified(문서상). 실장비 GET 으로 404 확인은 안 함 |
| BIOS XML 경로 | `GetCurrentBiosCfg --file bios.xml`; XML 은 `<BiosCfg><Menu><Setting name=.. selectedOption=.. type=Option>` 형식 | SUM User's Guide Rev 2.7.0 4.4 (https://www.thomas-krenn.com/de/wikiDE/images/1/17/SUM_UserGuide.pdf), Supermicro FAQ 28095 | verified |
| 최초 Redfish BMC 펌웨어 | 문서에는 "3.xx" 계열이라고만 있고 정확한 최초 빌드 번호 없음 | - | unavailable |

## 시도 내역
1차: Reference Guide 2.0a PDF 를 curl+pdftotext 로 직접 읽음, Rev 6.1 PDF 와 대조, 다운로드 피드 조회. 2차: SUM 2.4/2.7 가이드, 웹 검색(GitHub 덤프/서드파티)에서 X10 Redfish BIOS 속성 사례 없음. Jev 생략(키 없음).

## 실장비에서 확인하는 법
`curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Systems/1` 의 응답에 `Bios` 링크가 없으면 미지원. BIOS 값은 `sum -i <BMC> -u <USER> -p <PASSWORD> -c GetCurrentBiosCfg --file bios.xml`.
