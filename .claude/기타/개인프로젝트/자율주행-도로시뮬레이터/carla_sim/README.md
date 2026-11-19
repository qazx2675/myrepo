# CARLA 버전 (사실적인 그래픽)

기존 pygame 시뮬레이터의 기능을 [CARLA](https://carla.org) (언리얼 엔진 기반 자율주행 시뮬레이터) 위로 옮긴 버전입니다.
그래픽·차량 물리·트래픽은 CARLA 가 맡고, 이 폴더는 **조작·설정 메뉴·내비·주차·제로백·HUD·CUDA용 API** 를 담당합니다.

> 이 코드는 GPU 가 없는 환경에서 작성되어 **CARLA 서버에 붙여 실제로 돌려보지 못했습니다.**
> 서버 없이 확인 가능한 부분(30 km 직선 맵 생성, 경로 탐색, 제원→물리 변환)만 `tests/test_carla_offline.py` 로 검증했습니다.
> 처음 실행에서 오류가 나면 메시지를 그대로 알려주세요.

---

## 1) 설치 (Windows)

### PC 요구사항
| 항목 | 최소 | 권장 |
|---|---|---|
| GPU | NVIDIA GTX 1070 / VRAM 6 GB | RTX 3070 이상 / VRAM 8 GB+ |
| RAM | 16 GB | 32 GB |
| 디스크 | 25 GB (CARLA 본체) | SSD |
| OS | Windows 10/11 64bit | |
| Python | **3.11** (`wheels/carla-0.9.16-cp311-...whl` 이 3.11 용) | |

DirectX 12/Vulkan 을 지원하는 최신 NVIDIA 드라이버를 설치해 두세요. 나중에 CUDA 를 같은 PC 에서 돌릴 거라면
GPU 메모리를 CARLA 와 나눠 쓰므로 VRAM 8 GB 이상이 편합니다.

### 자동 설치 (권장)
프로젝트 폴더에서 더블클릭 또는:
```bat
install_carla.bat                                   :: D:\CARLA_0.9.16 에 설치
install_carla.bat -InstallDir E:\CARLA_0.9.16        :: 다른 위치
install_carla.bat -ZipPath C:\Down\CARLA_0.9.16.zip   :: 이미 받은 zip 으로 (다운로드 생략)
```
드라이브 여유 공간 확인(40 GB) → CARLA zip 다운로드(약 15 GB, 끊기면 다시 실행해 이어받기) → 압축 해제 → zip 삭제
→ `.venv` 에 numpy/pygame/moderngl/carla 설치(`wheels\` 에서, 인터넷 불필요) → `CARLA_ROOT` 환경변수 등록까지 합니다.
그 뒤에는 `run_carla.bat` 하나로 서버를 켜고 시뮬레이터까지 실행합니다 (`run_carla.bat --level long --cars 0` 처럼 옵션 전달).
서버는 창 없이(`-RenderOffScreen`) 뒤에서 돌기 때문에 게임 창 하나만 보입니다. 게임을 닫아도 서버는 남아 있어 다음 실행이 빠르고, 완전히 끄려면 `stop_carla.bat` 을 실행합니다.
다운로드 주소가 바뀌어 실패하면 아래 1번 링크에서 zip 을 직접 받아 `-ZipPath` 로 넘기면 됩니다.

**NVMe 와 SATA SSD 차이:** 맵 불러오기(시작, F1~F5)가 몇 초 빨라지는 정도이고, 주행 중 FPS 는 GPU 가 정하므로 체감 차이는 거의 없습니다.
HDD 는 맵 로딩이 1분 이상 걸리고 도심 맵에서 끊길 수 있어 피하는 게 좋습니다. 그래서 기본 설치 위치를 D 드라이브로 했습니다.

### 수동 설치 순서
1. CARLA **0.9.16** Windows 패키지 다운로드: <https://github.com/carla-simulator/carla/releases/tag/0.9.16>
   → `CARLA_0.9.16.zip` 을 `C:\CARLA_0.9.16` 같은 경로(한글·공백 없는 곳)에 압축 해제.
   Town04/Town05/Town10HD 는 기본 패키지에 들어 있습니다 (AdditionalMaps 는 필요 없음).
2. 이 프로젝트 폴더에서 패키지 설치 (인터넷 없이 `wheels/` 에서):
   ```bat
   setup.bat
   .venv\Scripts\pip install --no-index --find-links wheels -r requirements-carla.txt
   ```
3. CARLA 서버 실행 (창이 뜨고 도시가 보이면 준비 완료, 처음엔 1~2분 걸림):
   ```bat
   C:\CARLA_0.9.16\CarlaUE4.exe -quality-level=Epic
   ```
   GPU 가 약하면 `-quality-level=Low`. 서버 창은 관전용이라 최소화해 둬도 됩니다.

### 실행 시 "DirectX runtime" 오류가 나면
언리얼 엔진이 쓰는 DirectX 9 런타임(June 2010)이 없어서입니다 (CARLA 0.9.16 패키지에는 들어 있지 않음). 터미널에서:
```bat
winget install --id Microsoft.DirectX -e --force
winget install --id Microsoft.VCRedist.2015+.x64 -e
```
또는 microsoft.com 의 "DirectX End-User Runtime Web Installer". 최신 `install_carla.bat` 은 이것도 자동으로 확인·설치합니다.

## 2) 실행

**바탕화면 `Autodrive CARLA` 바로가기** 를 더블클릭하면 서버와 시뮬레이터가 함께 켜집니다 (설치 스크립트가 만들어 줌, 다시 만들려면 `make_shortcut.ps1` 실행). 초급/중급/고급·직선·주차 코스, 트래픽, 차종, 파워트레인, 제원, 시점, 소리는 모두 게임 안 시작 메뉴에서 고르며 실행 옵션은 필요 없습니다.

실행하면 먼저 **정비소(메인 화면)** 가 뜹니다. 내 차를 갓길(있으면)에 세우고 둘레의 가로등·표지판·가드레일·나무를 잠시 숨긴 채 카메라가 천천히 돌며, 왼쪽 메뉴에서 ↑↓ + Enter 로 고릅니다. 출발하면 숨긴 것들이 돌아오고 차는 원래 자리(다른 차가 와 있으면 같은 차로 앞쪽 빈 곳)로 갑니다.

| 메뉴 | 내용 |
|---|---|
| 출발 | 선택한 코스로 운전 시작 (W 가속, S 브레이크, A/D 조향) |
| 엔진 튜닝 | 파츠를 순정 → 스트리트 → 스포츠 → 레이스 단계로 장착. ↑↓ 파츠, ←→ 단계, Enter 장착. 오른쪽 성능 표에 바뀔 값이 초록/빨강으로 미리 보임 |
| 세부 조정 | 파츠를 단 뒤 엔진 출력·토크·레드라인·(모터 출력·토크·최고 회전)·종감속비(전기차는 감속비)·단별 기어비·타이어 접지력·브레이크 힘을 ←→ 0.5% 단위(Shift 5%, 레드라인은 50/500 rpm)로 조절, 범위 ±30%. R 원래대로. 성능 표에 조절 전과 비교가 바로 보이고, Esc 로 나가면 `config/garage.json` 에 파워트레인별로 저장하고 차에 적용 |
| 다이노 테스트 | CARLA 에 실제로 넣는 토크 곡선 그대로 출력(hp, 주황)·토크(Nm, 파랑) 그래프 (흐린 선 = 순정). **W 를 누르고 있는 만큼 직접 스로틀**을 밟아 엔진을 돌리고(떼면 공회전으로, 최고 회전에서는 회전 제한에 걸려 튕김), **Enter/Space = 풀스로틀 측정**(최고 회전까지 자동으로 밟았다 뗌). 풀스로틀 구간이 굵은 측정 곡선으로 그려지고 최고 출력·최대 토크와 그 회전수, 바퀴 출력 표시. R 그래프 지우기. 엔진음이 회전수·스로틀을 따라감 |
| 차량 선택 | 차종(승용차/버스/트럭), 동력 방식(엔진 자동/수동·전기·하이브리드) — 바로 차가 바뀜 |
| 코스 선택 | 초급/중급/고급/30 km 직선/주차장. Enter 로 불러와 바로 운전 |
| 설정 | 기존 설정 메뉴 (교통량·세부 제원·카메라·소리·언어) |
| 종료 | 시뮬레이터 종료 |

운전 중 **Esc = 정비소**, **Tab = 설정 메뉴**.

엔진 튜닝 파츠 (`sim/parts.py`, 장착 상태는 차종별로 `config/garage.json` 에 바로 저장):

| 파츠 | 대상 | 레이스 단계 효과 |
|---|---|---|
| 흡기 · 에어필터 | 엔진차·하이브리드 | 출력 +12% |
| 배기 시스템 | 엔진차·하이브리드 | 출력 +13%, 토크 +9% |
| 연료 인젝터 · 펌프 | 엔진차·하이브리드 | 출력 +10%, 토크 +8% |
| 캠샤프트 · 밸브 | 엔진차·하이브리드 | 출력 +10%, 레드라인 +1200 rpm |
| 터보차저 | 엔진차·하이브리드 | 출력 +60%, 토크 +65% |
| ECU 튜닝 | 엔진차·하이브리드 | 출력 +15%, 토크 +12%, 레드라인 +500 rpm |
| 전기 모터 | 전기차·하이브리드 | 모터 출력 +60%, 토크 +50% |
| 인버터 | 전기차·하이브리드 | 모터 출력 +15%, 모터 최고 회전 +30% (최고속도↑) |
| 배터리 팩 | 전기차·하이브리드 | 용량 +70% (늘어난 kWh 당 6 kg 무거워짐) |
| 타이어 (접지력) | 모든 차 | 접지력 +35% (스트리트 +10%, 스포츠 +22%) — 코너·제동이 좋아짐 (CARLA 에서는 출발 가속이 원래 접지 한계에 안 걸려 0-100 은 거의 그대로) |
| 흡입 팬 키트 (다운포스) | 전기차·하이브리드 | 팬카처럼 바닥 공기를 빨아들여 차를 노면에 붙이는 팬 + 추가 모터: 접지력 ×2.6, 모터 출력 ×2.65, 모터 토크 ×2.4. 전기 승용차에 전기 모터·인버터·타이어·팬을 모두 레이스로 달면 약 1,310 hp, 0-100 약 1.5초 |

**외관·소리도 파츠에 따라 바뀝니다.** 장착한 파츠 단계 합계에 따라 차 색이 순정 → 스트리트(초록) → 스포츠(파랑) → 레이스(빨강)로 바뀌고 (정비소 성능 표에 '외관' 단계 표시), 배기 = 엔진음이 크고 거칠게, 흡기 = 가속 시 흡입음, 터보 = 회전수에 따라 높아지는 휘슬 + 가속 페달을 뗄 때 블로오프 "치익", 전기 모터 = 모터음이 크고 높게 납니다. 엔진·모터·터보 소리는 매 프레임 실시간으로 합성해서 RPM 이 바뀌어도 음높이가 계단 없이 매끄럽게 이어집니다.

효과는 곱해서 쌓입니다 (승용차 엔진 파츠 전부 레이스 = 약 200 → 560 hp). 설정 메뉴의 제원은 순정 값이고 파츠는 그 위에 얹힙니다.
성능 표의 최고속도·0-100·0-200 은 간이 차량 모델(`sim/vehicle.py`)로 계산한 예상값이고, 실제 기록은 30 km 직선 코스에서 잴 수 있습니다. 버스·트럭도 속도 제한 장치가 없어서(설정 메뉴에서 켤 수는 있음) 파츠를 달면 최고속도가 올라갑니다.
메뉴·주행 화면·내비 안내는 한국어가 기본이고, 메뉴의 `언어 / Language` 항목에서 English 로 바꿀 수 있습니다 (문구 표: `sim/i18n.py`).

`run_carla.bat` (서버 자동 시작) 또는, 서버를 켠 상태에서 이 프로젝트 폴더에서:
```bat
.venv\Scripts\python carla_sim\run.py                                  :: Town04 고속도로, 중급
.venv\Scripts\python carla_sim\run.py --level long --cars 0            :: 30 km 직선, 제로백
.venv\Scripts\python carla_sim\run.py --level parking                  :: 평행 주차
.venv\Scripts\python carla_sim\run.py --level hard --vehicle truck --powertrain manual
.venv\Scripts\python carla_sim\run.py --controller examples\nav_follow.py   :: 외부 컨트롤러(K 로 on/off)
```
맵을 바꿀 때(첫 실행, F1~F5) CARLA 가 맵을 불러오느라 10~40초 멈춘 것처럼 보일 수 있습니다.

### 키
| 키 | 동작 |
|---|---|
| W / S, A / D (방향키도 됨) | 가속 / 브레이크, 조향 (키보드 페달 곡선은 기존과 동일) |
| G | 정차 중 D ↔ R |
| Z / X | 수동 변속기 기어 내림 / 올림 |
| M | MANUAL → ACC(속도·차간 자동) → AUTO (Traffic Manager 가 목적지 경로를 따라 운전) |
| [ / ] | AUTO 목표 속도 -10 / +10 km/h (긴 직선 AUTO 기본 300 km/h) |
| N | 다음 목적지 (경로 탐색 + 안내) |
| T / P | 차종 / 파워트레인 바꾸기 (그 자리에서) |
| V | 카메라: 운전석 → 추적 → 위에서 |
| F1~F5 | 초급 / 중급 / 고급 / 30 km 직선 / 주차 |
| Esc | **정비소** (메인 화면: 출발·엔진 튜닝·차량·코스·설정·종료) |
| Tab | **설정 메뉴** (기존과 같은 메뉴: 코스·트래픽·차종·파워트레인·마력/토크/레드라인/종감속비/브레이크/무게/타이어/조향/리미터·EV(감속비 포함)/하이브리드 제원, Save 로 `config/vehicles.json` 저장) |
| K | 외부 컨트롤러 on/off · Backspace 차 위치를 차로 중앙으로 · Space 일시정지 · H 도움말 |
| F11 | 전체화면 ↔ 창 (게임 해상도 그대로 모니터에 맞춰 늘림, 실행 옵션 `--fullscreen` 으로 처음부터 전체화면) |

차가 뒤집히거나 옆으로 누운 채 2초가 지나면 가까운 도로 차로 가운데(다른 차가 있으면 같은 차로 앞쪽 빈 곳)에 자동으로 다시 세웁니다.

### 옵션
| 옵션 | 기본값 | 설명 |
|---|---|---|
| `--level` | normal | `easy` `normal` `hard` `long` `parking` (아래 표) |
| `--vehicle` / `--powertrain` | car / auto | 기존과 같음 |
| `--cars` | 난이도별 | AI 트래픽 대수 |
| `--lanes` | 3 | 30 km 직선 코스 차로 수 |
| `--host` / `--port` | 127.0.0.1 / 2000 | CARLA 서버 주소 (다른 PC 에서 서버를 돌려도 됨) |
| `--width` / `--height` | 1280 / 720 | 창 = 카메라 해상도 |

| level | 맵 | 트래픽 | 성향 | 날씨 |
|---|---|---|---|---|
| easy | Town04 (고속도로 + 순환로) | 25 | 제한속도 -20%, 차간 4 m, 차선변경 없음 | 맑음 |
| normal | Town04 | 70 | 제한속도, 차간 2.5 m | 구름 |
| hard | Town05 (도심 + 고가, 교차로·신호) | 110 | 제한속도 +20%, 차간 1.2 m, 잦은 차선변경 | 비 온 뒤 노을 |
| long | 자동 생성 30 km 직선 (OpenDRIVE) | 20 | 100~160 km/h | 맑음 |
| parking | 자동 생성 주차장 | 0 | 직각 + 평행 주차칸 | 맑음 |

## 3) 30 km 직선 코스

CARLA 기본 맵에는 25 km 이상 직선이 없어서, `carla_sim/xodr.py` 가 **OpenDRIVE 직선 도로(30 km, 3차로 + 갓길)** 를
만들어 `client.generate_opendrive_world()` 로 불러옵니다. 300 km/h 로 5분 = 25 km + 가속·감속 여유 5 km.
- 차 쪽 속도 상한은 없습니다 (제원의 리미터 0 = 끔). 최고속도는 마력·기어비·공기저항으로 정해집니다.
- 도로 제한속도는 400 km/h 로 적어 둬서 AUTO/트래픽도 막히지 않습니다.
- 생성 맵은 도로 메시만 있고 건물·나무 같은 풍경은 없습니다 (CARLA 의 OpenDRIVE 단독 모드 제약).

## 4) CUDA 연결 API (`carla_sim/env.py`)

기존 `env.py` 와 같은 모양입니다. 행동 = `(throttle 0~1, brake 0~1, steer -1~1, +는 왼쪽)`.
```python
from carla_sim.env import CarlaDrivingEnv
env = CarlaDrivingEnv(level="normal", vehicle="car", powertrain="ev", frame_size=(640, 360))
obs, frame = env.reset()          # frame: (360, 640, 3) uint8 RGB, CARLA 카메라 (HUD 없음)
env.sim.set_destination(0)        # 내비 방식: obs["route"], obs["nav"]
obs, frame = env.step((0.5, 0.0, 0.0))
env.close()
```
동기 모드(1/60초 고정, 60 FPS)라 `step()` 1번 = 물리 1스텝 = 카메라 1프레임이 정확히 맞습니다.
`obs` 키: speed, steer_angle, max_steer, lane, lane_id, n_lanes, lane_width, lateral_offset, heading_error,
speed_limit, recommended_speed, lane_center / lane_left / lane_right (P,2), route (P,2), nav, curvature,
lead_gap, lead_speed, vehicles (K,6), gear, reverse, powertrain, vehicle_kind, collisions, time
(+ 주차 코스: parking, parking_target). 좌표는 모두 내 차 기준 (x 전방 +, y 왼쪽 +) [m].
기존 `examples/nav_follow.py` 를 `--controller` 로 그대로 쓸 수 있습니다.

## 5) 기존 기능 이식 현황

| 기능 | CARLA 버전 | 비고 |
|---|---|---|
| 사실적인 그래픽 (조명·반사·날씨·도시 풍경) | ✅ | CARLA/언리얼 |
| 다차선 곡선 도로, AI 트래픽 차간거리·차선변경 | ✅ | CARLA 맵 + Traffic Manager (IDM/MOBIL 대신 TM 로직) |
| 난이도 초/중/고 | ✅ | 맵·대수·속도·차간·차선변경·날씨 |
| 차종 승용차/버스/트럭 | ✅ | Lincoln MKZ (EV 는 Tesla Model 3) / Fusorosa 버스 / European HGV 트럭 |
| 마력·토크·브레이크·무게·타이어·조향각 설정 | ✅ | CARLA 물리(PhysX)에 토크곡선·기어비·질량·브레이크토크·타이어마찰로 적용 |
| 내연기관 자동/수동 | ✅ | 수동은 Z/X 로 직접 기어 |
| 전기 | ✅ | 모터 토크/출력 곡선 + 1단 감속기, 발 떼면 회생제동, 배터리 잔량(HUD), 방전 시 출력 차단 |
| 하이브리드 (엔진/모터 마력 따로) | ⚠️ 근사 | 엔진 곡선 + 모터 토크를 한 축에 더함, 회생·배터리 잔량, 저속 EV 주행은 배터리 계산에만 반영 |
| 속도 리미터 | ✅ | |
| 시뮬 안 설정 메뉴 | ✅ | 기존 메뉴 그대로 (카메라 효과/소리 항목은 CARLA 에서는 동작 안 함) |
| 정비소 메인 화면 + 엔진 튜닝 파츠 | ✅ | CARLA 버전 전용 (pygame 버전에는 없음) |
| 목적지 + 경로 안내 | ✅ | 맵 토폴로지 다익스트라, 좌/우회전·차로 변경 안내, 미니맵 |
| AUTO 모드 | ✅ | Traffic Manager 가 경로 따라 운전 |
| ACC (속도·차간만 자동) | ✅ | M 으로 MANUAL → ACC → AUTO, [ ] 로 목표 속도, 브레이크 밟으면 사람 우선 |
| 30 km 직선 + 제로백 0-100/200/300 | ✅ | 300 km/h 상한 없음 |
| 주차 | ✅ | 전용 주차장 맵(자동 생성): 직각 주차칸 + 평행 주차칸, 칸 크기는 내 차에 맞춤, 목표 칸 초록 표시, N = 다음 칸 |
| 운전석/추적/위에서 시점 | ✅ | |
| 카메라 연출, 절차적 사운드 | ✅ | 운전석 시점: 가감속 피칭·코너 롤·코너 안쪽 보기·속도 진동 (시야각 변화는 없음). 엔진/모터/타이어/바람 소리 |
| RPM 바, 배터리 표시 | ✅ | |
| 헤드리스 통계 / 스크린샷 옵션 | ❌ | 서버가 렌더링하므로 기존 pygame 버전 사용 |
| CUDA API (내비 방식 + 화면 캡처 방식) | ✅ | `carla_sim/env.py` |

기존 pygame 버전(`main.py`)은 그대로 남아 있어서, GPU 없는 PC 나 빠른 대량 시험에는 계속 쓸 수 있습니다.
