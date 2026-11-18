# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| main.py | 실행 진입점: 옵션 파싱, 창/키보드 루프, 헤드리스·스크린샷 모드 |
| env.py | 외부(CUDA) 컨트롤러 인터페이스 `DrivingEnv`: 관측값(obs) + 화면 프레임 + 행동 입력, `make_world` |
| sim/track.py | 도로 기하: 본선(`Track`), 진출로(`Ramp`), Frenet 변환, 제한속도/권장속도, 나무·건물·표지판 배치, 코스 형태(난이도별, 30 km 직선) |
| sim/world.py | 시뮬레이션 상태(SoA numpy 배열) + 한 스텝 진행: AI 트래픽, 차선 변경, 내 차 본선/진출로 판정, 가드레일, 충돌, 급제동 이벤트 |
| sim/vehicle.py | 내 차 동역학: 엔진/변속기/전기/하이브리드, 브레이크, 타이어, 후진, 제로백 타이머 |
| sim/controllers.py | 선행차 탐색 + IDM (N대 배열 연산, CUDA 이식 대상), 순수추종 조향 |
| sim/nav.py | 내비게이션: 목적지, 안내 문구, 경로 좌표 |
| sim/scenario.py | 난이도(초/중/고급) 설정값 |
| sim/parking.py | 주차장 시나리오: 주차칸/주차된 차, 충돌, 성공 판정, 주차 관측값 |
| config/vehicles.json | 차종별 제원 (마력, 토크, 브레이크, 질량, 타이어, 전기/하이브리드) |
| viz/render3d.py | 운전석/추적 시점 원근 렌더러 + 카메라·차체 연출 + 풍경 |
| viz/gl3d.py | OpenGL(ModernGL) 3D 렌더러: 조명·그림자·안개·하늘, 차량 인스턴싱 |
| viz/scene.py | 렌더러 선택(gl / classic), OpenGL 불가 시 classic 대체 |
| install_carla.bat / .ps1 | CARLA 자동 설치 (D 드라이브 기본) + 파이썬 패키지 |
| run_carla.bat | CARLA 서버 시작 + carla_sim/run.py 실행 |
| carla_sim/run.py | CARLA 버전 실행 파일: 창, 키 입력, HUD, 미니맵, 설정 메뉴 연결 |
| carla_sim/sim.py | CARLA 월드 래퍼: 맵/날씨/Traffic Manager/내 차/카메라/주차/내비/관측값 |
| carla_sim/physics.py | vehicles.json 제원 → CARLA VehiclePhysicsControl |
| carla_sim/route.py | 맵 토폴로지 경로 탐색 + 안내 문구 |
| carla_sim/xodr.py | 30 km 직선 OpenDRIVE 생성 |
| carla_sim/env.py | CARLA 버전 CUDA 연결 API |
| viz/menu.py | 시뮬레이터 안 설정 메뉴(Tab/Esc) |
| viz/topdown.py | 위에서 보는 2D 뷰 |
| viz/hud.py | 속도계/RPM/기어, 제한속도, 미니맵, 내비 배너, 정보창, 제로백 기록 |
| viz/sound.py | 코드로 생성하는 엔진/모터/타이어/바람 소리 |
| examples/ | 외부 컨트롤러 예제: `nav_follow.py`(경로 데이터), `vision_lane.py`(화면 캡처) |
| tests/test_sim.py | 기능 테스트 |
| wheels/ | 오프라인 설치 파일 (Python 3.11, Windows/Linux) |
| setup.sh / setup.bat | 오프라인 설치 스크립트 |

수정 요청별로 볼 곳:
- "차 성능/느낌" → `config/vehicles.json`, `sim/vehicle.py`
- "AI 차량 행동/난이도" → `sim/scenario.py`, `sim/world.py`(`_lane_changes`, `_brake_events`), `sim/controllers.py`
- "도로 모양/출구 추가" → `sim/track.py`(`layout_for`, 진출로 배치)
- "화면/HUD" → `viz/`
- "CUDA 컨트롤러 연결" → `env.py`, `examples/`

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
