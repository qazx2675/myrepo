"""차량 제어 로직 (CPU / numpy 벡터화 버전).

모든 함수는 "차량 N대 상태 배열 -> 결과 배열" 형태라서, 나중에 CuPy 나 Numba CUDA
커널로 같은 시그니처를 유지한 채 교체할 수 있다. (N×N 선행차 탐색은 GPU에서
스레드 1개 = 차량 1대로 그대로 옮기기 좋은 구조)
"""
import math

import numpy as np


def find_leaders(s, d, target_d, length, width, L, max_range=250.0):
    """각 차량 i의 같은 차로 선행차 j와 범퍼 간 거리(gap)를 구한다.

    j가 i의 선행차가 되는 조건: i 앞쪽(0 < ds < max_range)이고,
    차체가 i와 옆으로 겹치거나 i가 들어가려는 차로(target_d)에 걸쳐 있음.
    반환: leader[N] (없으면 -1), gap[N] (없으면 inf)
    """
    ds = np.mod(s[None, :] - s[:, None], L)                 # [i, j] = i에서 j까지 앞쪽 거리
    gap = ds - 0.5 * (length[:, None] + length[None, :])
    overlap = np.abs(d[None, :] - d[:, None]) < 0.5 * (width[:, None] + width[None, :]) + 0.3
    in_target = np.abs(d[None, :] - target_d[:, None]) < 0.5 * width[None, :] + 0.9
    mask = (overlap | in_target) & (ds > 0) & (ds < max_range)
    np.fill_diagonal(mask, False)
    gap = np.where(mask, gap, np.inf)
    leader = np.argmin(gap, axis=1)
    g = gap[np.arange(len(s)), leader]
    leader = np.where(np.isfinite(g), leader, -1)
    return leader, g


def idm_accel(v, v0, gap, v_lead, a_max, b, T, s0):
    """Intelligent Driver Model 가속도 (배열/스칼라 모두 가능). gap=inf 이면 자유주행."""
    v = np.asarray(v, dtype=float)
    dv = v - np.asarray(v_lead, dtype=float)
    s_star = s0 + np.maximum(0.0, v * T + v * dv / (2.0 * np.sqrt(a_max * b)))
    free = 1.0 - (v / np.maximum(v0, 0.1)) ** 4
    gap = np.asarray(gap, dtype=float)
    with np.errstate(divide="ignore", invalid="ignore"):
        inter = np.where(np.isfinite(gap), (s_star / np.maximum(gap, 0.1)) ** 2, 0.0)
    return np.clip(a_max * (free - inter), -9.0, a_max)


class IDMController:
    """종방향(가감속) 컨트롤러. obs(dict of arrays) -> accel[N].

    향후 CUDA 컨트롤러도 같은 obs 키를 받아 accel 배열을 돌려주면 World 에 그대로 꽂힌다.
    """

    def __call__(self, obs):
        leader, gap = find_leaders(obs["s"], obs["d"], obs["target_d"],
                                   obs["length"], obs["width"], obs["track_length"])
        v_lead = np.where(leader >= 0, obs["v"][leader], obs["v"])
        acc = idm_accel(obs["v"], obs["v0"], gap, v_lead,
                        obs["a_max"], obs["b"], obs["T"], obs["s0"])
        return acc, leader, gap


def pure_pursuit(track, ego, target_d, wheelbase):
    """목표 차로 중심선을 따라가는 조향각 [rad]."""
    s, _ = track.project(ego.x, ego.y, s_hint=ego.s)
    ld = 6.0 + 0.8 * ego.v
    tx, ty, _ = track.to_world(s + ld, target_d)
    dx, dy = float(tx) - ego.x, float(ty) - ego.y
    c, sn = math.cos(ego.yaw), math.sin(ego.yaw)
    lx, ly = dx * c + dy * sn, -dx * sn + dy * c
    alpha = math.atan2(ly, lx)
    return math.atan2(2.0 * wheelbase * math.sin(alpha), math.hypot(lx, ly))
