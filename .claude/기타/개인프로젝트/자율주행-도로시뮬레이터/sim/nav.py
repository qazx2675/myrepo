"""내비게이션: 목적지(진출로 끝) 설정, 남은 거리, 차로 안내, 경로 좌표.

본선은 순환 고속도로이고 목적지는 각 진출로(IC) 끝이다. 경로 = 본선을 따라 해당 출구까지 →
맨 오른쪽 차로로 이동 → 진출로 진입 → 진출로 끝. 출구를 놓치면 한 바퀴 돌아 재탐색된다.
"""
import numpy as np

PREPARE_DIST = 1500.0   # 이 거리 안에서 출구 차로로 이동 안내


class Navigator:
    def __init__(self, track):
        self.track = track
        self.dest = None          # 목적지 진출로 인덱스
        self.arrived = False
        self.arrived_at = None

    @property
    def dest_ramp(self):
        return None if self.dest is None else self.track.ramps[self.dest]

    def set_destination(self, k):
        self.dest = k
        self.arrived = False
        self.arrived_at = None

    def cycle_destination(self):
        n = len(self.track.ramps)
        self.set_destination(0 if self.dest is None else (self.dest + 1 if self.dest + 1 < n else None))

    # ------------------------------------------------------------------
    def dist_to_exit(self, ego):
        r = self.dest_ramp
        if r is None or ego.road is not None:
            return None
        u = (ego.s - r.s0) % self.track.L
        if u <= r.taper:          # 이미 분기 구간 안 (음수 = 출구 지점을 지난 거리)
            return -float(u)
        return float((r.s0 - ego.s) % self.track.L)

    def guidance(self, ego):
        """HUD/외부 컨트롤러용 안내 정보 dict."""
        t, r = self.track, self.dest_ramp
        n = t.n_lanes
        g = dict(active=r is not None, destination=None if r is None else r.name,
                 distance=None, distance_to_exit=None, required_lane=None,
                 instruction="No destination (press N)", arrow="none", arrived=self.arrived)
        if ego.road is not None and ego.s > ego.road.L - 15.0:
            self.arrived = True
            self.arrived_at = ego.road.name
            ok = ego.road is r
            g.update(arrived=True, distance=0.0, arrow="flag",
                     instruction=f"Arrived: {ego.road.name}" + ("" if ok or r is None else " (wrong exit)")
                     + "  - press N for next destination")
            return g
        if r is None:
            return g
        if ego.road is not None:
            if ego.road is r:
                g.update(distance=float(r.L - ego.s), arrow="up",
                         instruction=f"Follow ramp to {r.name}  {r.L - ego.s:.0f} m")
            else:
                g.update(arrow="u", instruction=f"Wrong exit ({ego.road.name}) - drive to end and press N")
            return g
        de = self.dist_to_exit(ego)
        lane = int(t.lane_of(ego.d))
        g.update(distance=max(de, 0) + r.L, distance_to_exit=de)
        if de > PREPARE_DIST:
            g.update(arrow="up", instruction=f"Go straight {de / 1000:.1f} km, then exit right to {r.name}")
        elif lane < n - 1:
            g.update(required_lane=n - 1, arrow="right_lane",
                     instruction=f"Exit in {max(de, 0):.0f} m - move to lane {n} (right)")
        else:
            g.update(required_lane=n - 1, arrow="exit",
                     instruction=f"Exit right in {max(de, 0):.0f} m  -> {r.name}")
        return g

    # ------------------------------------------------------------------
    def route_xy(self, ego, dists, lane):
        """내 차에서 경로를 따라 dists[m] 앞 지점들의 월드 좌표 (x, y)."""
        t = self.track
        dists = np.asarray(dists, float)
        if ego.road is not None:
            x, y, _ = ego.road.to_world(ego.s + dists, 0.0)
            return x, y
        x, y, _ = t.to_world(ego.s + dists, t.lane_d(lane))
        r = self.dest_ramp
        if r is not None and lane == t.n_lanes - 1:
            de = self.dist_to_exit(ego)
            if de < PREPARE_DIST:
                u = dists - de
                rx, ry, _ = r.to_world(np.maximum(u, 0.0), 0.0)
                x = np.where(u >= 0, rx, x)
                y = np.where(u >= 0, ry, y)
        return x, y
