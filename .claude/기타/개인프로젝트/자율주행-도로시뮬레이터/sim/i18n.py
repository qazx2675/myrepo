"""화면 글자 한국어/영어 전환. 기본은 한국어이고, 설정 메뉴의 '언어' 항목으로 바꾼다.

코드 안의 문구는 영어 원문으로 두고 tr("영어 원문") 으로 감싼다. 한국어일 때 KO 표에 있으면 바꿔 쓰고,
없으면 원문 그대로 보인다. {이름} 자리는 tr("...", 이름=값) 으로 채운다.
"""
LANG = "ko"
LANGS = ("ko", "en")
LANG_NAME = {"ko": "한국어", "en": "English"}

KO = {
    # 설정 메뉴
    "SETTINGS": "설정",
    ">>> START / RESUME DRIVING  (Enter)  <<<": ">>> 운전 시작 / 계속하기  (Enter)  <<<",
    "SCENARIO": "주행 코스",
    "VEHICLE": "차량",
    "DISPLAY / SOUND": "화면 / 소리",
    "Course / difficulty": "코스 / 난이도",
    "Traffic cars": "다른 차량 수",
    "Lanes": "차로 수 (직선 코스)",
    "Random seed": "랜덤 시드",
    "Vehicle type": "차종",
    "Powertrain": "동력 방식",
    "Camera view": "카메라 시점",
    "Camera effects": "카메라 흔들림 효과",
    "Sound": "소리",
    "Key help overlay": "조작키 도움말",
    "Language": "언어 / Language",
    "> Save vehicle settings to config/vehicles.json": "> 차량 설정 저장 (config/vehicles.json)",
    "> Reload settings from file": "> 저장된 설정 다시 불러오기",
    "> Quit simulator": "> 시뮬레이터 종료",
    "Up/Down select   Left/Right change (Shift = big step)   Enter = run   Tab/Esc = close":
        "↑↓ 항목 선택   ←→ 값 바꾸기 (Shift = 크게)   Enter = 실행   Tab/Esc = 닫기",
    "Reloaded from config/vehicles.json": "config/vehicles.json 에서 다시 불러왔습니다",
    "Saved to config/vehicles.json": "config/vehicles.json 에 저장했습니다",
    "on": "켬", "off": "끔", "auto": "자동",
    "cockpit": "운전석", "chase": "뒤에서 따라가기", "top": "위에서 보기",
    # 코스 / 차량
    "Easy (beginner)": "초급 (쉬움)", "Normal": "중급 (보통)", "Hard (expert)": "고급 (어려움)",
    "Long straight 30 km": "30 km 직선 (제로백)", "Parking lot": "주차장",
    "Sedan": "승용차", "Bus": "버스", "Truck": "트럭",
    "Engine - automatic": "엔진 - 자동변속", "Engine - manual": "엔진 - 수동변속",
    "Electric": "전기차", "Hybrid": "하이브리드",
    # 차량 제원
    "Engine power [hp]": "엔진 출력 [마력]",
    "Engine torque [Nm]": "엔진 토크 [Nm]",
    "Redline [rpm]": "최고 회전수 (레드라인) [rpm]",
    "Final drive (lower=faster)": "종감속비 (낮을수록 최고속도↑)",
    "Brake force [N]": "브레이크 힘 [N]",
    "Mass [kg]": "차량 무게 [kg]",
    "Tire grip mu": "타이어 접지력 (μ)",
    "Max steer [deg]": "최대 조향각 [도]",
    "Speed limiter [km/h] (0=off)": "속도 제한 장치 [km/h] (0=끔)",
    "EV motor power [kW]": "전기차 모터 출력 [kW]",
    "EV motor torque [Nm]": "전기차 모터 토크 [Nm]",
    "EV reduction (lower=faster)": "전기차 감속비 (낮을수록 최고속도↑)",
    "EV battery [kWh]": "전기차 배터리 [kWh]",
    "Hybrid engine power [hp]": "하이브리드 엔진 출력 [마력]",
    "Hybrid motor power [hp]": "하이브리드 모터 출력 [마력]",
    # 로딩 / 오류 화면
    "AUTODRIVE ROAD SIM - CARLA": "자율주행 도로 시뮬레이터 - CARLA",
    "Connecting to CARLA server ...": "CARLA 서버에 연결하는 중 ...",
    "Loading course: {name} ...": "코스 불러오는 중: {name} ...",
    "Switching to: {name} ...": "코스 바꾸는 중: {name} ...",
    "(first map load can take 10-60 s)": "(처음 맵을 불러올 때는 10~60초 걸릴 수 있습니다)",
    "Cannot connect to CARLA server at {host}:{port}": "CARLA 서버({host}:{port})에 연결할 수 없습니다",
    "Start D:\\CARLA_0.9.16\\CarlaUE4.exe (or run_carla.bat) and try again.":
        "바탕화면 'Autodrive CARLA' 바로가기(또는 run_carla.bat)로 다시 실행하십시오.",
    "Press any key to close.": "아무 키나 누르면 닫힙니다.",
    # 주행 화면
    "MANUAL": "수동", "ACC": "ACC (차간거리 유지)", "AUTO": "자율주행", "EXTERNAL": "외부 컨트롤러",
    "MODE  {mode}   [{level}]": "모드  {mode}   [{level}]",
    "MAP   {map}   VIEW {view}": "맵  {map}   시점  {view}",
    "LANE  {lane} / {n}   offset {off:+.2f} m": "차로  {lane} / {n}   치우침 {off:+.2f} m",
    "LEAD  {gap:.0f} m": "앞차  {gap:.0f} m",
    "LEAD  -": "앞차  -",
    "COLLISION {c}   TRAFFIC {t}": "충돌 {c}   주변 차량 {t}",
    "ODO {km:.2f} km   {fps:.0f} FPS": "주행거리 {km:.2f} km   {fps:.0f} FPS",
    "   PAUSED": "   일시정지",
    "limit {v:.0f}": "제한 {v:.0f}",
    "  set {v:.0f}": "  설정 {v:.0f}",
    "BAT {p:4.0f}%": "배터리 {p:.0f}%",
    "0-100 / 200 / 300 km/h": "제로백  0-100 / 200 / 300 km/h",
    "now": "이번", "best": "최고",
    "PARKED!": "주차 완료!",
    "{kind} parking (N = next)": "{kind} 주차 (N = 다음 칸)",
    "along {a:+.1f} m  side {s:+.2f} m  yaw {y:.0f}": "앞뒤 {a:+.1f} m  좌우 {s:+.2f} m  각도 {y:.0f}°",
    "parallel": "평행", "perpendicular": "직각",
    "W/S gas/brake  A/D steer": "W/S 가속/브레이크  A/D 조향",
    "G D<->R  Z/X gear (manual)": "G 전진/후진  Z/X 기어 (수동)",
    "M manual -> ACC -> AUTO": "M 수동 -> ACC -> 자율주행",
    "N next destination": "N 다음 목적지",
    "[ ] AUTO set speed": "[ ] 자율주행 설정 속도",
    "T vehicle  P powertrain": "T 차종  P 동력 방식",
    "V camera": "V 카메라 시점",
    "F1-F5 level  Tab/Esc settings": "F1-F5 코스  Tab/Esc 설정 메뉴",
    "K external controller": "K 외부 컨트롤러",
    "Backspace reset  H help": "Backspace 처음 위치  H 도움말",
    # 내비게이션
    "No destination (press N)": "목적지 없음 (N 키로 선택)",
    "Arrived: {dest}": "도착: {dest}",
    "Turn left": "좌회전", "Turn right": "우회전",
    "Move to left lane": "왼쪽 차로로 이동", "Move to right lane": "오른쪽 차로로 이동",
    "{what} in {d:.0f} m": "{d:.0f} m 앞 {what}",
    "Go straight  {km:.1f} km to {dest}": "직진  {dest} 까지 {km:.1f} km",
    "Parked! N = next space": "주차 완료! N = 다음 칸",
    "Park in: {dest}": "주차할 곳: {dest}",
    "Parking space {i}/{n} ({kind})": "주차 칸 {i}/{n} ({kind})",
    "Lot exit": "주차장 출구",
    "End of straight": "직선 도로 끝",
}


def set_lang(lang):
    global LANG
    LANG = lang if lang in LANGS else "ko"


def tr(text, /, **kw):
    t = KO.get(text, text) if LANG == "ko" else text
    return t.format(**kw) if kw else t


# 한글이 나오는 글꼴 (Windows: 맑은 고딕). pygame 은 쉼표로 적은 목록에서 처음 찾은 것을 쓴다.
FONTS = "malgungothic,applegothic,nanumgothic,notosanscjkkr,notosanscjk,wenquanyizenhei,dejavusans,arial"


def font(size, bold=False):
    import pygame
    return pygame.font.SysFont(FONTS, size, bold=bold)
