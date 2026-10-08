# 구버전 펌웨어 차이 (B200 M5 / B480 M5, 최신 = 4.1(3h) 가이드 기준)
출처: Cisco UCS M5 Server BIOS Tokens (3.2) https://www.cisco.com/c/en/us/td/docs/unified_computing/ucs/ucs-manager/Reference-Docs/Server-BIOS-Tokens/3-2/b_UCS_BIOS_Tokens.html , 4.0 / 4.1 Tokens 가이드.
- 3.2(1): B200 M5 만. Workload Configuration 값 = NUMA/UMA (이후 Balanced/IO Sensitive). XPT Prefetch, Sub NUMA Clustering, LLC Prefetch 기본 Enabled(이후 Disabled). IPV6 PXE Support 기본 Enabled(이후 Disabled). IMC Interleave 값 1-way/2-way 만(기본 1-way; 이후 Auto 추가).
- 3.2(2): B480 M5 추가. Boot Option Retry 계열(Number of Retries, Cool Down Time, Boot Option Retry) 블레이드 지원.
- 3.2(3): Energy Efficient Turbo, Autonomous Core C-state, ProcessorEppProfile, Patrol Scrub 추가. Memory RAS 값 Mirror Mode 1LM / Maximum Performance. Memory Mapped IO above 4GB 기본값이 3.2(3) 문서 내부에서 Enabled/Disabled 불일치 (unknown).
- 4.0(2a): Adaptive Memory Training, OptionROM Launch Optimization, BIOS Techlog Level 추가.
- 4.0(4a): Select Memory RAS 에 ADDDC Sparing 추가, 4.0(4c) 기본값 Maximum Performance -> ADDDC Sparing. Processor Prefetch Config 에 Auto 추가. Intel Speed Select, DCPMM Firmware Downgrade 추가. 4.0 표기 CDN Control 기본 Disabled (4.1 은 Enabled).
- 4.1(1a): Partial Mirror 계열, Memory Size Limit, PCIe RAS, P-SATA AHCI 값 등.
- 4.1(2): Memory Refresh Rate(기본 2x), Panic and High Watermark, PCIe PLL SSC, Configurable TDP Level, Uncore Frequency Scaling, UPI Link Frequency Select, External SSC Enable, CR QoS, NVM Performance Setting, CR FastGo Config, Snoopy mode for 2LM/AD 추가.
- 4.1(3a): Memory Refresh Rate 기본 1x, Memory Thermal Throttling Mode, Advanced Memory Test 추가. 4.1(3e): Burst and Postponed Refresh. 4.1(3h): SHA-1 / SHA-256 PCR Bank.
- 삭제/이름변경 이력은 문서에서 명시 확인 불가(표기 차이는 있으나 동일 토큰으로 추정).
