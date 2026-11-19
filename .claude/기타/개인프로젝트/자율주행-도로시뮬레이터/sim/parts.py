"""정비소 튜닝 파츠: 엔진/모터 부품을 단계별(순정·스트리트·스포츠·레이스)로 장착해 제원을 올린다.

- 장착 상태는 차종별로 config/garage.json 에 저장된다 ({"car": {"turbo": 2, ...}, ...}).
- 원래 제원(config/vehicles.json, 설정 메뉴에서 고치는 값)은 그대로 두고, 차를 만들 때 apply_parts() 로
  파츠 효과를 얹은 제원을 쓴다. 엔진 파츠는 엔진차·하이브리드, 전기 파츠는 전기차·하이브리드에만 적용.
- performance() 는 sim/vehicle.py 의 차량 모델로 직선 풀가속을 돌려 0-100, 최고속도를 예상한다
  (CARLA 물리와 조금 다를 수 있음. 실제 기록은 30 km 직선 코스에서 측정).
"""
import copy
import functools
import json
import os

CONFIG = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "config", "garage.json")
LEVELS = ("Stock", "Street", "Sport", "Race")          # 순정 / 스트리트 / 스포츠 / 레이스
ICE = ("auto", "manual", "hybrid")
ELEC = ("ev", "hybrid")
KG_PER_KWH = 6.0                                       # 배터리 팩 무게 (늘린 용량만큼 차가 무거워짐)

# (키, 이름, 적용 파워트레인, 단계별 효과) — power/torque/motor_* /battery 는 배수, redline 은 rpm 더하기
PARTS = [
    ("intake", "Intake & air filter", ICE,
     [{}, {"power": 1.04}, {"power": 1.08}, {"power": 1.12}]),
    ("exhaust", "Exhaust system", ICE,
     [{}, {"power": 1.04, "torque": 1.03}, {"power": 1.08, "torque": 1.06}, {"power": 1.13, "torque": 1.09}]),
    ("fuel", "Fuel injectors & pump", ICE,
     [{}, {"power": 1.03, "torque": 1.03}, {"power": 1.06, "torque": 1.05}, {"power": 1.10, "torque": 1.08}]),
    ("camshaft", "Camshaft & valves", ICE,
     [{}, {"power": 1.03, "redline": 300}, {"power": 1.06, "redline": 700}, {"power": 1.10, "redline": 1200}]),
    ("turbo", "Turbocharger", ICE,
     [{}, {"power": 1.15, "torque": 1.20}, {"power": 1.35, "torque": 1.40}, {"power": 1.60, "torque": 1.65}]),
    ("ecu", "ECU tuning", ICE,
     [{}, {"power": 1.05, "torque": 1.05}, {"power": 1.10, "torque": 1.08, "redline": 300},
      {"power": 1.15, "torque": 1.12, "redline": 500}]),
    ("motor", "Electric motor", ELEC,
     [{}, {"motor_power": 1.15, "motor_torque": 1.15}, {"motor_power": 1.35, "motor_torque": 1.30},
      {"motor_power": 1.60, "motor_torque": 1.50}]),
    ("inverter", "Inverter", ELEC,
     [{}, {"motor_power": 1.05, "motor_rpm": 1.10}, {"motor_power": 1.10, "motor_rpm": 1.20},
      {"motor_power": 1.15, "motor_rpm": 1.30}]),
    ("battery", "Battery pack", ELEC,
     [{}, {"battery": 1.2}, {"battery": 1.4}, {"battery": 1.7}]),
]
BY_KEY = {p[0]: p for p in PARTS}


def applicable(powertrain):
    """이 파워트레인에 달 수 있는 파츠 목록."""
    return [p for p in PARTS if powertrain in p[2]]


def load_parts(path=CONFIG):
    """{차종: {파츠키: 단계}}. 파일이 없거나 깨졌으면 모두 순정."""
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return {}
    return {k: {p: int(lv) for p, lv in v.items() if p in BY_KEY} for k, v in data.items() if isinstance(v, dict)}


def save_parts(parts, path=CONFIG):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(parts, f, indent=2, ensure_ascii=False)


def apply_parts(spec, installed, powertrain):
    """원래 제원 + 장착 파츠 효과 = 새 제원 (원본은 바꾸지 않음)."""
    s = copy.deepcopy(spec)
    for key, lv in (installed or {}).items():
        if key not in BY_KEY or powertrain not in BY_KEY[key][2]:
            continue
        fx = BY_KEY[key][3][max(0, min(len(LEVELS) - 1, lv))]
        if "power" in fx:
            s["power_hp"] *= fx["power"]
            s["hybrid"]["engine_hp"] *= fx["power"]
        if "torque" in fx:
            s["torque_nm"] *= fx["torque"]
            s["hybrid"]["engine_torque_nm"] *= fx["torque"]
        if "redline" in fx:
            r = s["redline_rpm"]
            s["redline_rpm"] = r + fx["redline"]
            s["shift_up_rpm"] = round(s["shift_up_rpm"] * s["redline_rpm"] / r)   # 변속 시점도 같이
        if "motor_power" in fx:
            s["ev"]["motor_power_kw"] *= fx["motor_power"]
            s["hybrid"]["motor_hp"] *= fx["motor_power"]
        if "motor_torque" in fx:
            s["ev"]["motor_torque_nm"] *= fx["motor_torque"]
            s["hybrid"]["motor_torque_nm"] *= fx["motor_torque"]
        if "motor_rpm" in fx:
            s["ev"]["max_motor_rpm"] *= fx["motor_rpm"]
            s["hybrid"]["max_motor_rpm"] *= fx["motor_rpm"]
        if "battery" in fx:
            b = s[powertrain]["battery_kwh"]
            s[powertrain]["battery_kwh"] = b * fx["battery"]
            s["mass"] += (b * fx["battery"] - b) * KG_PER_KWH
    return s


def system_power(spec, powertrain):
    """(시스템 최고출력 hp, 최대토크 Nm) — 하이브리드는 엔진 + 모터."""
    if powertrain == "ev":
        return spec["ev"]["motor_power_kw"] * 1.341, spec["ev"]["motor_torque_nm"]
    if powertrain == "hybrid":
        h = spec["hybrid"]
        return h["engine_hp"] + h["motor_hp"], h["engine_torque_nm"] + h["motor_torque_nm"]
    return spec["power_hp"], spec["torque_nm"]


@functools.lru_cache(maxsize=256)
def _perf(spec_json, kind, powertrain):
    from sim.vehicle import Vehicle
    v = Vehicle(json.loads(spec_json), kind, powertrain)
    v.auto_shift = True                    # 수동도 계산할 때는 제때 변속
    if powertrain == "hybrid":
        v.battery_kwh = v.battery_cap
    dt, t, t100, t200, still = 0.1, 0.0, None, None, 0.0
    vmax = 0.0
    while t < 150.0:
        v.step(1.0, 0.0, 0.0, dt)
        t += dt
        if t100 is None and v.vx * 3.6 >= 100:
            t100 = t
        if t200 is None and v.vx * 3.6 >= 200:
            t200 = t
        if v.vx > vmax + 0.01:
            vmax, still = v.vx, 0.0
        else:
            still += dt
            if still > 4.0:                # 더 이상 빨라지지 않음 = 최고속도
                break
    return t100, t200, vmax * 3.6


def performance(spec, kind, powertrain):
    """화면 표시용 성능 요약 dict: power_hp, torque_nm, top_kmh, t100/t200(초, 못 가면 None), mass, kg_per_hp,
    limited(속도 제한 장치에 걸린 최고속도면 True)."""
    hp, nm = system_power(spec, powertrain)
    t100, t200, top = _perf(json.dumps(spec, sort_keys=True), kind, powertrain)
    lim = spec.get("speed_limiter_kmh", 0)
    return dict(power_hp=hp, torque_nm=nm, top_kmh=top, t100=t100, t200=t200, mass=spec["mass"],
                kg_per_hp=spec["mass"] / hp, limited=bool(lim) and top >= lim - 1)
