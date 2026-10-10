# X11 vs X10 (BIOS / Redfish)

속성 단위 diff: **불가** (두 세대 모두 속성 목록 unavailable). API 수준 차이만 기록 (출처: Redfish Reference Guide 2.0a, 2019-02).

| 항목 | X10 (BMC 3.xx) | X11 (BMC 1.xx) |
|---|---|---|
| Redfish | 있음 | 있음 |
| `Systems/1/Bios`, `Bios/SD`, `ResetBios`, `ChangePassword` | 없음 | X11DP 만 ("only X11DP supports") |
| `UpdateService/SmcFirmwareInventory` | 표기 없음 | "Supported on X11 platforms" |
| BIOS 설정 XML(SUM) | 사용 | 사용 |
