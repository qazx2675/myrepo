"""내 차(ego) 차량 동역학.

외부 물리엔진 없이 가벼운 자체 모델:
  - 종방향: 엔진 토크 곡선(마력/토크) + 변속기(자동/수동), 전기모터(단일 감속 + 회생제동),
            하이브리드(엔진 + 모터, 저속은 모터만, 가속 시 합산, 감속 시 회생),
            브레이크 힘, 공기저항, 구름저항, 타이어 마찰 한계(휠스핀/잠김)
  - 횡방향: 동적 자전거 모델(선형 코너링 강성 + 마찰 한계로 포화) → 고속 급조향 시 미끄러짐
            저속(< 3 m/s)은 키네마틱 모델과 섞어서 안정화
제원은 config/vehicles.json 에서 읽는다.
"""
import json
import math
import os

G = 9.81
RHO = 1.2
POWERTRAINS = ("auto", "manual", "ev", "hybrid")   # 내연기관 자동 / 수동 / 전기 / 하이브리드
CONFIG = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "config", "vehicles.json")


def load_specs(path=CONFIG):
    with open(path, encoding="utf-8") as f:
        return {k: v for k, v in json.load(f).items() if not k.startswith("_")}


class Vehicle:
    def __init__(self, spec, kind="car", powertrain="auto", x=0.0, y=0.0, yaw=0.0):
        self.spec, self.kind, self.powertrain = spec, kind, powertrain
        sp = spec
        self.length, self.width, self.height = sp["length"], sp["width"], sp["height"]
        self.wheelbase = sp["wheelbase"]
        self.m = sp["mass"]
        self.b = sp["wheelbase"] * sp["front_weight"]        # 무게중심 -> 뒤축
        self.a = sp["wheelbase"] - self.b                    # 무게중심 -> 앞축
        self.Iz = self.m * self.a * self.b
        self.rw = sp["wheel_radius"]
        self.x, self.y, self.yaw = x, y, yaw
        self.vx = self.vy = self.r = 0.0
        self.steer = 0.0
        self.gear = 1
        self.rpm = sp["idle_rpm"] if powertrain != "ev" else 0.0
        self.ax = 0.0
        self.wheelspin = self.abs_active = self.sliding = False
        self.s, self.d = 0.0, 0.0      # 현재 도로 기준 Frenet
        self.road = None               # None = 본선, 아니면 Ramp
        self.battery_cap = sp["hybrid"]["battery_kwh"] if powertrain == "hybrid" else sp["ev"]["battery_kwh"]
        self.battery_kwh = self.battery_cap * (0.6 if powertrain == "hybrid" else 1.0)
        self.power_kw = 0.0          # 배터리 출력 [kW] (+ 방전, - 충전)
        self.engine_on = powertrain != "ev"
        self._f_motor = 0.0
        self.eng = dict(sp)          # 엔진 제원 (하이브리드는 엔진 마력/토크를 따로 씀)
        if powertrain == "hybrid":
            self.eng.update(power_hp=sp["hybrid"]["engine_hp"], torque_nm=sp["hybrid"]["engine_torque_nm"])
        self.offroad = False
        self.auto_shift = False      # 수동 변속기라도 자동주행 모드에서는 자동 변속
        self.reverse = False         # 후진 기어 (R)

    # ------------------------------------------------------------------
    @property
    def v(self):
        return math.hypot(self.vx, self.vy)

    def max_steer(self):
        """키보드/외부입력 steer=1 일 때 바퀴 조향각 [rad] (속도 감응: 고속일수록 작게)."""
        return math.radians(self.spec["max_steer_deg"]) / (1.0 + (self.vx / 12.0) ** 2)

    def toggle_reverse(self):
        """정차 상태에서만 D <-> R 전환."""
        if abs(self.vx) < 0.5:
            self.reverse = not self.reverse
            self.vx = self.vy = self.r = 0.0
            self.gear = 1

    def shift(self, dg):
        if self.powertrain == "manual":
            self.gear = max(1, min(len(self.spec["gears"]), self.gear + dg))

    @property
    def soc(self):
        return self.battery_kwh / self.battery_cap

    # ---------- 파워트레인 ----------
    def _engine_torque(self, rpm):
        sp = self.eng
        lo, hi = sp["torque_peak_rpm"]
        tq = sp["torque_nm"]
        if rpm < lo:   # 저회전: 최대토크의 60%에서 선형 상승
            t = tq * (0.6 + 0.4 * (rpm - sp["idle_rpm"]) / max(lo - sp["idle_rpm"], 1))
        else:
            t = tq
        p_max = sp["power_hp"] * 745.7
        t = min(t, p_max / max(rpm * 2 * math.pi / 60, 1.0))   # 출력 한계
        if rpm >= sp["redline_rpm"]:
            t = 0.0   # 레브리미터
        return max(t, 0.0)

    def _motor_force(self, throttle, torque_nm, power_w, reduction, max_rpm):
        """전기모터 구동력 [N], 모터 회전수."""
        w_m = max(self.vx / self.rw * reduction, 1.0)
        rpm = w_m * 60 / (2 * math.pi)
        t = 0.0 if rpm >= max_rpm else min(torque_nm, power_w / w_m)
        return throttle * t * reduction / self.rw, rpm

    def _drive_force(self, throttle):
        """구동력 [N] (+ 회생/엔진브레이크는 음수)와 회전수 갱신."""
        sp = self.spec
        self._f_motor = 0.0
        coasting = throttle < 0.05 and self.vx > 1.0
        if self.powertrain == "ev":
            ev = sp["ev"]
            f, self.rpm = self._motor_force(throttle, ev["motor_torque_nm"], ev["motor_power_kw"] * 1000,
                                            ev["reduction"], ev["max_motor_rpm"])
            f = -ev["coast_regen_n"] if coasting else f      # 페달 뗐을 때 약한 회생
            self._f_motor = f
            return f
        if self.powertrain == "hybrid":
            hy = sp["hybrid"]
            ok = self.soc > 0.05
            f_m, rpm_m = self._motor_force(throttle if ok else 0.0, hy["motor_torque_nm"],
                                           hy["motor_hp"] * 745.7, hy["motor_reduction"], hy["max_motor_rpm"])
            if coasting:
                self.engine_on = False
                self._f_motor = -hy["coast_regen_n"]
                self.rpm = 0.0
                return self._f_motor
            ev_only = self.vx * 3.6 < hy["ev_only_below_kmh"] and throttle < 0.6 and self.soc > 0.15
            self._f_motor = f_m
            if ev_only:
                self.engine_on = False
                self.rpm = 0.0
                return f_m
            self.engine_on = True
            return self._ice_force(throttle, coasting=False) + f_m
        return self._ice_force(throttle, coasting)

    def _ice_force(self, throttle, coasting):
        sp = self.eng
        w_wheel = self.vx / self.rw
        gears = sp["gears"]
        if self.powertrain in ("auto", "hybrid") or self.auto_shift:
            rpm_now = w_wheel * gears[self.gear - 1] * sp["final_drive"] * 60 / (2 * math.pi)
            if rpm_now > sp["shift_up_rpm"] and self.gear < len(gears):
                self.gear += 1
            elif rpm_now < sp["shift_down_rpm"] and self.gear > 1:
                self.gear -= 1
        ratio = gears[self.gear - 1] * sp["final_drive"]
        rpm = w_wheel * ratio * 60 / (2 * math.pi)
        # 출발 시 클러치 반클러치 자동 처리: 회전수는 아이들 이하로 안 떨어짐
        self.rpm = max(sp["idle_rpm"] * (1 + 0.8 * throttle) if rpm < sp["idle_rpm"] * 1.5 else 0.0, rpm)
        if rpm > sp["redline_rpm"] * 1.15:      # 수동에서 저단 과회전 -> 엔진브레이크 강하게
            return -0.3 * self._engine_torque(sp["torque_peak_rpm"][0]) * ratio / self.rw
        t = throttle * self._engine_torque(self.rpm)
        if coasting:   # 엔진 브레이크
            t = -0.12 * sp["torque_nm"] * min(1.0, self.rpm / sp["redline_rpm"] * 2)
        return t * ratio * sp["drivetrain_eff"] / self.rw

    # ------------------------------------------------------------------
    def step(self, throttle, brake, steer_target, dt, offroad=False):
        sp = self.spec
        self.offroad = offroad
        ms = math.radians(sp["max_steer_deg"])
        steer_target = max(-ms, min(ms, steer_target))
        rate = math.radians(sp["steer_rate_deg"]) * dt
        self.steer += max(-rate, min(rate, steer_target - self.steer))
        if self.reverse:
            self._step_reverse(throttle, brake, dt)
            return

        mu = sp["tire_mu"] * (0.6 if offroad else 1.0)
        f_drive = self._drive_force(throttle)
        lim = sp["speed_limiter_kmh"]
        if lim and self.vx * 3.6 > lim and f_drive > 0:
            f_drive = 0.0
        f_brake = brake * sp["brake_force_n"]
        regen = 0.0
        if self.powertrain in ("ev", "hybrid") and brake > 0 and self.vx > 1.0:
            regen = min(f_brake, sp[self.powertrain]["regen_force_n"])

        # 마찰 한계 (구동은 뒤/앞 한 축 하중, 제동은 전체 하중)
        f_drive_max = mu * self.m * G * 0.55
        self.wheelspin = f_drive > f_drive_max
        f_drive = min(f_drive, f_drive_max)
        f_brake_max = mu * self.m * G
        self.abs_active = f_brake > f_brake_max
        f_brake = min(f_brake, f_brake_max)

        f_res = 0.5 * RHO * sp["cd_a"] * self.vx * abs(self.vx) + \
            sp["c_roll"] * (6.0 if offroad else 1.0) * self.m * G * (1 if self.vx > 0.05 else 0)
        fx = f_drive - f_res - (f_brake if self.vx > 0.01 else 0.0)

        # 배터리 (모터 효율 0.9, 회생 효율 0.7)
        if self.powertrain in ("ev", "hybrid"):
            p_mech = self._f_motor * self.vx
            p_el = p_mech / 0.9 if p_mech > 0 else p_mech * 0.7
            p_el -= regen * self.vx * 0.7
            if self.powertrain == "ev":
                p_el += sp["ev"]["aux_kw"] * 1000
            elif self.engine_on and self.soc < 0.5:
                p_el -= 4000.0        # 엔진이 돌 때 발전기로 충전
            self.power_kw = p_el / 1000
            self.battery_kwh = min(self.battery_cap,
                                   max(0.0, self.battery_kwh - self.power_kw * dt / 3600))
            if self.powertrain == "ev" and self.battery_kwh <= 0:
                fx = min(fx, -f_res)

        n = 4
        h = dt / n
        Fzf = self.m * G * self.b / self.wheelbase
        Fzr = self.m * G * self.a / self.wheelbase
        for _ in range(n):
            vx = self.vx
            ax = fx / self.m
            vx_new = max(0.0, vx + ax * h)
            self.ax = (vx_new - vx) / h
            if vx_new < 3.0:   # 저속: 키네마틱
                k = vx_new / 3.0
                r_kin = vx_new * math.tan(self.steer) / self.wheelbase
                vy_kin = r_kin * self.b
                self.r = k * self.r + (1 - k) * r_kin
                self.vy = k * self.vy + (1 - k) * vy_kin
                self.sliding = False
            if vx_new >= 1.0:
                af = self.steer - (self.vy + self.a * self.r) / max(vx_new, 1.0)
                ar = -(self.vy - self.b * self.r) / max(vx_new, 1.0)
                Fyf_lin = sp["cornering_front"] * af
                Fyr_lin = sp["cornering_rear"] * ar
                Fyf = max(-mu * Fzf, min(mu * Fzf, Fyf_lin))
                Fyr = max(-mu * Fzr, min(mu * Fzr, Fyr_lin))
                self.sliding = abs(Fyr_lin) > mu * Fzr * 1.05 or abs(Fyf_lin) > mu * Fzf * 1.05
                vy_dot = (Fyf * math.cos(self.steer) + Fyr) / self.m - vx_new * self.r
                r_dot = (self.a * Fyf * math.cos(self.steer) - self.b * Fyr) / self.Iz
                self.vy += vy_dot * h
                self.r += r_dot * h
            self.vx = vx_new
            if self.vx == 0.0:
                self.vy = self.r = 0.0
            c, s = math.cos(self.yaw), math.sin(self.yaw)
            self.x += (self.vx * c - self.vy * s) * h
            self.y += (self.vx * s + self.vy * c) * h
            self.yaw += self.r * h

    def _step_reverse(self, throttle, brake, dt):
        """후진: 저속 키네마틱 모델, 최고 20 km/h."""
        sp = self.spec
        self.rpm = sp["idle_rpm"] * (1 + throttle) if self.powertrain != "ev" else -self.vx / self.rw * 60
        f = throttle * 0.25 * self.m * G if -self.vx * 3.6 < 20 else 0.0
        a = -f / self.m
        v = self.vx + a * dt
        if v < 0:   # 제동/구름저항은 0 을 향해
            stop = (brake * sp["brake_force_n"] / self.m + sp["c_roll"] * G * 3) * dt
            v = min(0.0, v + stop)
        self.ax = (v - self.vx) / dt
        self.vx = v
        self.r = v * math.tan(self.steer) / self.wheelbase
        self.vy = self.r * self.b
        c, s = math.cos(self.yaw), math.sin(self.yaw)
        self.x += (self.vx * c - self.vy * s) * dt
        self.y += (self.vx * s + self.vy * c) * dt
        self.yaw += self.r * dt
        self.wheelspin = self.abs_active = self.sliding = False

    # ------------------------------------------------------------------
    def pedals_for_accel(self, a_des):
        """원하는 가속도 -> (throttle, brake). ACC/자동주행에서 사용."""
        sp = self.spec
        f_res = 0.5 * RHO * sp["cd_a"] * self.vx ** 2 + sp["c_roll"] * self.m * G
        f_need = self.m * a_des + f_res
        if f_need >= 0:
            # 현재 기어/회전수에서 낼 수 있는 최대 구동력으로 정규화
            saved = (self.gear, self.rpm, self.engine_on, self._f_motor)
            f_full = max(self._drive_force(1.0), 1.0)
            self.gear, self.rpm, self.engine_on, self._f_motor = saved
            return min(1.0, f_need / f_full), 0.0
        return 0.0, min(1.0, -f_need / sp["brake_force_n"])

    def hit_rail(self, x, y, yaw):
        """가드레일 접촉: 도로 쪽으로 밀어내고 횡속도 제거, 감속."""
        self.x, self.y = x, y
        self.yaw = yaw
        self.vy = self.r = 0.0
        self.steer = 0.0
        self.vx *= 0.6


class AccelTimer:
    """정지 상태에서 출발해 100 / 200 / 300 km/h 에 도달하는 시간 측정 (제로백)."""
    MARKS = (100, 200, 300)

    def __init__(self):
        self.t0 = None
        self.results = {}      # 이번 측정
        self.best = {}         # 최고 기록
        self.armed = True

    def update(self, v_kmh, now):
        if v_kmh < 1.0:
            self.t0 = None
            self.armed = True
            return
        if self.t0 is None and self.armed:
            self.t0 = now
            self.results = {}
            self.armed = False
        if self.t0 is None:
            return
        for m in self.MARKS:
            if m not in self.results and v_kmh >= m:
                t = now - self.t0
                self.results[m] = t
                self.best[m] = min(self.best.get(m, t), t)
