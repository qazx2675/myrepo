# 구버전 펌웨어 차이 (14G, iDRAC9 3.x/4.x)

출처: iDRAC9 Attribute Registry PDF "New features added" 장 (https://dl.dell.com/topicspdf/idrac9-lifecycle-controller-v4x-series_Reference-Guide_en-us.pdf)

BIOS 속성 관련 추가 이력(최신 4.40.00.00 기준 이전 버전에는 없음):
- 4.40.00.00: BIOS.BiosBootSettings.SysPrepClean 추가
- 4.30.30.30: BIOS.SysInformation.AgesaVersion 추가 (AMD 플랫폼용; Intel 14G 에는 해당 없을 수 있음)
- 4.22.00.00 / 4.20.20.20 / 4.10.10.10 / 4.00.00.00: 해당 장에 BIOS.* 추가 항목은 위 외에 명시되지 않음(iDRAC/NIC/InfiniBand 등 비-BIOS 속성).

삭제/이름변경/허용값 변경: 문서에 기재 없음 -> 확인불가(unknown). 3.x(3.00~3.4x) 레지스트리는 확보하지 못함.
