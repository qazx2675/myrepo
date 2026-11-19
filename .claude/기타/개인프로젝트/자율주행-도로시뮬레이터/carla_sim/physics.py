"""config/vehicles.json 제원 → CARLA VehiclePhysicsControl.

CARLA(언리얼 PhysX) 가 실제 차량 동역학을 계산하고, 여기서는 그 입력값만 바꾼다.
  - 엔진: 마력·토크로 토크 곡선(rpm→Nm) 생성, 레드라인, 기어비, 종감속비
  - 전기: 모터 토크(정토크) + 출력 제한 곡선, 1단 감속기
  - 하이브리드: 엔진 곡선 + 모터 토크를 같은 축에 더한 근사 (CARLA 에 모터 모델이 없어서)
  - 브레이크 힘 → 바퀴당 브레이크 토크, 타이어 mu → tire_friction, 최대 조향각, 무게, 공기저항
"""
import math

import carla

RPM_TO_RADS = 2 * math.pi / 60


def _power_limited(rpm, torque, power_w):
    return min(torque, power_w / max(rpm * RPM_TO_RADS, 1e-3))


def engine_curve(power_hp, torque_nm, idle_rpm, redline_rpm, n=14):
    """[(rpm, Nm)] : 저회전은 최대토크의 60% 에서 시작, 이후 최대토크와 출력 한계 중 작은 값."""
    pts = []
    for k in range(n):
        rpm = redline_rpm * k / (n - 1)
        t = _power_limited(rpm, torque_nm, power_hp * 745.7)
        if rpm < idle_rpm:
            t = torque_nm * (0.6 + 0.4 * rpm / idle_rpm)
        pts.append((rpm, t))
    return pts


def motor_curve(power_w, torque_nm, max_rpm, n=14):
    return [(max_rpm * k / (n - 1), _power_limited(max_rpm * k / (n - 1), torque_nm, power_w)) for k in range(n)]


def curve_for(spec, powertrain):
    """파워트레인별 (토크곡선, max_rpm, 기어비 리스트, 종감속비)."""
    if powertrain == "ev":
        e = spec["ev"]
        return (motor_curve(e["motor_power_kw"] * 1000, e["motor_torque_nm"], e["max_motor_rpm"]),
                e["max_motor_rpm"], [e["reduction"]], 1.0)
    if powertrain == "hybrid":
        h = spec["hybrid"]
        eng = engine_curve(h["engine_hp"], h["engine_torque_nm"], spec["idle_rpm"], spec["redline_rpm"])
        curve = [(rpm, t + _power_limited(rpm, h["motor_torque_nm"], h["motor_hp"] * 745.7)) for rpm, t in eng]
        return curve, spec["redline_rpm"], spec["gears"], spec["final_drive"]
    return (engine_curve(spec["power_hp"], spec["torque_nm"], spec["idle_rpm"], spec["redline_rpm"]),
            spec["redline_rpm"], spec["gears"], spec["final_drive"])


def apply_specs(vehicle, spec, powertrain):
    """vehicle(carla.Vehicle) 에 제원을 적용한다."""
    pc = vehicle.get_physics_control()
    curve, max_rpm, gears, final = curve_for(spec, powertrain)
    pc.torque_curve = [carla.Vector2D(x=float(r), y=float(t)) for r, t in curve]
    pc.max_rpm = float(max_rpm)
    up = spec["shift_up_rpm"] / spec["redline_rpm"]
    down = spec["shift_down_rpm"] / spec["redline_rpm"]
    pc.forward_gears = [carla.GearPhysicsControl(ratio=float(g), down_ratio=down, up_ratio=up) for g in gears]
    pc.final_ratio = float(final)
    pc.use_gear_autobox = powertrain != "manual"
    if powertrain == "ev":
        # 전기 모터는 회전 관성이 작고 엔진브레이크가 없다. CARLA 기본값(관성 1.0, 페달 뗄 때 감쇠 2.0)을
        # 감속비 9 로 바퀴에 걸면 차가 약 800 kg 무거운 것처럼 굼뜨고, 페달을 떼면 급제동처럼 선다.
        # (페달 뗄 때 감속은 sim.py 의 회생제동이 따로 맡음)
        pc.moi = 0.1
        pc.damping_rate_full_throttle = 0.02
        pc.damping_rate_zero_throttle_clutch_engaged = 0.05
        pc.damping_rate_zero_throttle_clutch_disengaged = 0.05
    pc.mass = float(spec["mass"])
    pc.drag_coefficient = float(spec["cd_a"] / (0.85 * spec["width"] * spec["height"]))
    wheels = pc.wheels
    brake = spec["brake_force_n"] * spec["wheel_radius"] / len(wheels)
    for i, w in enumerate(wheels):
        w.max_brake_torque = float(brake)
        w.tire_friction = float(3.5 * spec["tire_mu"])
        if i < 2:                                   # 앞바퀴
            w.max_steer_angle = float(spec["max_steer_deg"])
    pc.wheels = wheels
    vehicle.apply_physics_control(pc)
    return pc
