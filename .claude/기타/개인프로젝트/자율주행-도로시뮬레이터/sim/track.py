"""도로 기하.

- Road  : 중심선을 등간격(ds)으로 샘플링한 numpy 배열(x, y, psi, kappa, 제한속도) + Frenet 변환
- Track : 닫힌 순환 고속도로 본선 (직선/원호 구간으로 정의), 진출로(Ramp) 목록 포함
- Ramp  : 본선 맨 오른쪽 차로에서 갈라져 나가는 1차로 진출로. 끝이 목적지(IC)

Frenet 좌표: s = 중심선을 따라간 거리, d = 중심선에서 왼쪽(+) 횡방향 거리.
차로 번호는 한국식으로 0 = 1차로(가장 왼쪽, 추월차로), n-1 = 가장 오른쪽 차로.
"""
import math
from dataclasses import dataclass

import numpy as np


@dataclass(frozen=True)
class Seg:
    length: float      # 구간 길이 [m]
    angle: float = 0.0  # 회전각 [deg], + = 좌회전, 0 = 직선

    @property
    def radius(self):
        return math.inf if self.angle == 0 else self.length / math.radians(abs(self.angle))


def straight(length):
    return Seg(length, 0.0)


def arc(radius, deg):
    return Seg(radius * math.radians(abs(deg)), deg)


def layout_for(level="normal"):
    """코스 형태 (모두 좌회전 순환, 끝점이 정확히 닫히도록 대칭 구성)."""
    if level == "long":      # 장거리 직선: 약 8.3 km 직선 x2 (100 km/h 로 5분) - 제로백/최고속 측정용
        return [straight(30000), arc(400, 180), straight(30000), arc(400, 180)]
    if level == "easy":      # 큰 타원: 반경 400 m (100 km/h)
        return [straight(800), arc(400, 180), straight(800), arc(400, 180)]
    if level == "hard":      # 급곡선: 반경 120~180 m (50~60 km/h), 큰 S자
        s_curve = [arc(180, 30), arc(180, -60), arc(180, 30)]
        return ([straight(500)] + s_curve + [straight(300)]
                + [arc(120, 90), straight(80), arc(120, 90)]
                + [straight(300)] + s_curve + [straight(500)] + [arc(160, 180)])
    # normal: 약 4.2 km, S자 + 반경 200/250 m 곡선 (70~80 km/h)
    s_curve = [arc(400, 20), arc(400, -40), arc(400, 20)]
    return ([straight(500)] + s_curve + [straight(300)]
            + [arc(200, 90), straight(100), arc(200, 90)]
            + [straight(300)] + s_curve + [straight(500)] + [arc(250, 180)])


DEST_NAMES = ["Seoul IC", "Rest Area", "Logistics Hub", "Harbor IC", "Airport IC", "Stadium IC"]


def _wrap(a):
    return (a + np.pi) % (2 * np.pi) - np.pi


class Road:
    """등간격 샘플 중심선 + 차로 정보. closed=True 이면 순환."""

    def __init__(self, xs, ys, ps, ss, lims, closed, n_lanes, lane_width, ds, b_comf=1.0):
        self.closed = closed
        self.n_lanes = n_lanes
        self.lane_width = lane_width
        self.half_width = n_lanes * lane_width / 2.0   # 차도 가장자리까지 [m]
        self.shoulder = 1.8                            # 갓길 폭 [m]
        self.rail_d = self.half_width + 2.0            # 가드레일 위치 [m]
        L = ss[-1]
        self.N = int(round(L / ds)) + (0 if closed else 1)
        self.ds = L / (self.N if closed else self.N - 1)
        self.L = L
        self.s = np.arange(self.N) * self.ds
        self.x = np.interp(self.s, ss, xs)
        self.y = np.interp(self.s, ss, ys)
        self.psi = np.interp(self.s, ss, ps)
        self.kappa = np.gradient(self.psi, self.ds)
        idx = np.clip(np.searchsorted(ss, self.s, side="right") - 1, 0, len(lims) - 1)
        self.limit_kmh = np.asarray(lims, float)[idx]   # 표지판 제한속도

        # 권장 주행속도: 곡선 앞에서 미리 감속하도록 감속도 b_comf 프로파일
        v = self.limit_kmh / 3.6
        vref = v.copy()
        for _ in range(2 if closed else 1):
            for i in range(self.N - 1, -1, -1):
                j = i + 1
                if j >= self.N:
                    if not closed:
                        continue
                    j = 0
                vref[i] = min(v[i], math.sqrt(vref[j] ** 2 + 2 * b_comf * self.ds))
        self.v_ref = vref   # [m/s]

    # ---------- 좌표 변환 ----------
    def _wrap_s(self, s):
        s = np.asarray(s, dtype=float)
        return np.mod(s, self.L) if self.closed else np.clip(s, 0.0, self.L - 1e-6)

    def _idx(self, s):
        return (self._wrap_s(s) / self.ds).astype(np.int64) % self.N

    def pose(self, s):
        """s(스칼라/배열) -> 중심선 (x, y, psi). 선형 보간."""
        s = self._wrap_s(s)
        f = s / self.ds
        i = f.astype(np.int64) % self.N
        j = (i + 1) % self.N if self.closed else np.minimum(i + 1, self.N - 1)
        t = f - np.floor(f)
        x = self.x[i] + (self.x[j] - self.x[i]) * t
        y = self.y[i] + (self.y[j] - self.y[i]) * t
        psi = self.psi[i] + _wrap(self.psi[j] - self.psi[i]) * t
        return x, y, psi

    def to_world(self, s, d):
        x, y, psi = self.pose(s)
        d = np.asarray(d, dtype=float)
        return x - d * np.sin(psi), y + d * np.cos(psi), psi

    def project(self, x, y, s_hint=None, window=60.0):
        """월드 (x, y) -> Frenet (s, d). s_hint가 있으면 근처만 탐색."""
        if s_hint is None:
            idx = np.arange(self.N)
        else:
            c = int(s_hint / self.ds)
            w = int(window / self.ds)
            idx = np.arange(c - w, c + w + 1)
            idx = idx % self.N if self.closed else np.unique(np.clip(idx, 0, self.N - 1))
        d2 = (self.x[idx] - x) ** 2 + (self.y[idx] - y) ** 2
        i = idx[int(np.argmin(d2))]
        dx, dy = x - self.x[i], y - self.y[i]
        c, sn = math.cos(self.psi[i]), math.sin(self.psi[i])
        s = float(self.s[i] + dx * c + dy * sn)
        s = s % self.L if self.closed else s
        return s, float(-dx * sn + dy * c)

    # ---------- 차로 / 속도 ----------
    def lane_d(self, lane):
        return ((self.n_lanes - 1) / 2.0 - np.asarray(lane)) * self.lane_width

    def lane_of(self, d):
        return np.clip(np.rint((self.n_lanes - 1) / 2.0 - np.asarray(d) / self.lane_width),
                       0, self.n_lanes - 1).astype(int)

    def limit_at(self, s):
        return float(self.limit_kmh[int(self._idx(s))])

    def vref_at(self, s):
        return self.v_ref[self._idx(s)]

    def curvature_preview(self, s, horizon=100.0, step=5.0):
        """s부터 horizon 앞까지의 곡률 배열."""
        return self.kappa[self._idx(s + np.arange(0.0, horizon, step))]


class Ramp(Road):
    """본선 s0 지점의 맨 오른쪽 차로에서 갈라지는 진출로. 처음 taper 구간은 본선 위에 겹쳐 있다."""

    def __init__(self, main, s0, name, taper=180.0, radius=180.0, turn=-70.0, tail=250.0,
                 limit=60.0):
        self.main, self.s0, self.name, self.taper = main, s0, name, taper
        d_r = float(main.lane_d(main.n_lanes - 1))
        shift = main.lane_width + 0.6
        u = np.arange(0.0, taper + 1e-9, 1.0)
        k = u / taper
        d = d_r - shift * (3 * k ** 2 - 2 * k ** 3)          # smoothstep 으로 부드럽게 이탈
        xs, ys, _ = main.to_world(s0 + u, d)
        xs, ys = list(xs), list(ys)
        _, _, p = main.pose(s0 + taper)
        p = float(p)
        h = 1.0
        for seg in (arc(radius, turn), straight(tail)):
            kk = math.radians(seg.angle) / seg.length
            for _ in range(int(seg.length / h)):
                xs.append(xs[-1] + h * math.cos(p + kk * h / 2))
                ys.append(ys[-1] + h * math.sin(p + kk * h / 2))
                p += kk * h
        xs, ys = np.asarray(xs), np.asarray(ys)
        seg_len = np.hypot(np.diff(xs), np.diff(ys))
        ss = np.concatenate([[0.0], np.cumsum(seg_len)])
        ps = np.unwrap(np.arctan2(np.diff(ys), np.diff(xs)))
        ps = np.concatenate([ps, ps[-1:]])
        super().__init__(xs, ys, ps, ss, [limit], False, 1, main.lane_width, 0.5)
        self.rail_d = self.half_width + 1.4


class Track(Road):
    """닫힌 순환 본선 + 진출로(목적지) 목록."""

    def __init__(self, layout=None, n_lanes=3, lane_width=3.6, ds=0.5,
                 a_lat=2.0, v_cap_kmh=100.0, b_comf=1.0, seed=7, level="normal"):
        layout = layout or layout_for(level)
        total_turn = sum(seg.angle for seg in layout)
        if abs(abs(total_turn) - 360.0) > 1e-6:
            raise ValueError(f"트랙 회전각 합이 360도가 아님: {total_turn}")

        # 구간별로 촘촘히 적분
        xs, ys, ps, ss, lims, straights = [0.0], [0.0], [0.0], [0.0], [], []
        for seg in layout:
            if seg.angle == 0:
                straights.append((ss[-1], seg.length))
            n = max(1, int(math.ceil(seg.length / (ds / 4))))
            h = seg.length / n
            k = math.radians(seg.angle) / seg.length
            lim = v_cap_kmh if seg.angle == 0 else min(
                v_cap_kmh, math.floor(math.sqrt(a_lat * seg.radius) * 3.6 / 10) * 10)
            for _ in range(n):
                p = ps[-1]
                xs.append(xs[-1] + h * math.cos(p + k * h / 2))
                ys.append(ys[-1] + h * math.sin(p + k * h / 2))
                ps.append(p + k * h)
                ss.append(ss[-1] + h)
                lims.append(lim)
        xs, ys, ps, ss = map(np.asarray, (xs, ys, ps, ss))
        L = ss[-1]
        # 수치 적분 오차로 생긴 미세한 끝점 불일치를 길이에 비례해 분산
        self.closure_error = math.hypot(xs[-1] - xs[0], ys[-1] - ys[0])
        xs = xs - (xs[-1] - xs[0]) * ss / L
        ys = ys - (ys[-1] - ys[0]) * ss / L
        lims.append(lims[-1])
        super().__init__(xs, ys, ps, ss, lims, True, n_lanes, lane_width, ds, b_comf)

        # 진출로: 300 m 이상 직선마다 300 m 이상 간격으로 (직선당 최대 3개, 출발점 근처 제외)
        self.ramps = []
        for st, ln in straights:
            cnt = min(3, int(ln // 300))
            for k in range(cnt):
                s0 = st + 60 + (ln - 300) / max(cnt - 1, 1) * k if cnt > 1 else st + 60
                if s0 < 200:
                    continue
                self.ramps.append(Ramp(self, s0, DEST_NAMES[len(self.ramps) % len(DEST_NAMES)]))

        # 도로변 나무 (렌더링용 배경, 진출로 위는 제외)
        rng = np.random.default_rng(seed)
        ts = np.arange(0, L, 22.0) + rng.uniform(0, 10, int(np.ceil(L / 22.0)))
        ts = ts[ts < L]
        side = rng.choice([-1.0, 1.0], len(ts))
        td = side * (self.rail_d + rng.uniform(5, 35, len(ts)))
        tx, ty, _ = self.to_world(ts, td)
        keep = np.ones(len(ts), bool)
        for r in self.ramps:
            dist2 = (tx[:, None] - r.x[None, ::10]) ** 2 + (ty[:, None] - r.y[None, ::10]) ** 2
            keep &= dist2.min(axis=1) > 12.0 ** 2
        self.trees = np.stack([ts, td, tx, ty, rng.uniform(2.5, 4.5, len(ts)),
                               rng.uniform(6, 11, len(ts))], axis=1)[keep]

        # 건물 (도로에서 떨어진 곳에 가끔): s, d, x, y, 길이, 폭, 높이, 색 번호
        bs = np.arange(150, L, 260.0) + rng.uniform(0, 120, int(np.ceil((L - 150) / 260.0)))
        bs = bs[(bs < L) & (rng.random(len(bs)) < 0.55)]
        bd = rng.choice([-1.0, 1.0], len(bs)) * (self.rail_d + rng.uniform(45, 110, len(bs)))
        bx, by, _ = self.to_world(bs, bd)
        keep = np.ones(len(bs), bool)
        for r in self.ramps:
            dist2 = (bx[:, None] - r.x[None, ::10]) ** 2 + (by[:, None] - r.y[None, ::10]) ** 2
            keep &= dist2.min(axis=1) > 45.0 ** 2
        for k in range(len(bs)):   # 순환 코스의 다른 구간과 겹치지 않게
            keep[k] &= abs(self.project(bx[k], by[k])[1]) > self.rail_d + 30
        n_b = len(bs)
        self.buildings = np.stack([bs, bd, bx, by, rng.uniform(15, 40, n_b), rng.uniform(12, 25, n_b),
                                   rng.uniform(8, 45, n_b), rng.integers(0, 5, n_b)], axis=1)[keep]
        # 출구 안내 표지판 (출구 1 km / 300 m 전, 오른쪽)
        self.signs = [((r.s0 - k) % L, r.name, k) for r in self.ramps for k in (1000, 300)]

    def in_exit_zone(self, s, extra=120.0):
        """s 가 어떤 진출로의 분기 구간(오른쪽 가드레일 없음)에 있는지."""
        for r in self.ramps:
            if 0.0 <= (s - r.s0) % self.L <= r.taper + extra:
                return True
        return False
