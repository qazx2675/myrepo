"""정비소 파츠 테스트: python -m pytest tests -q"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from sim import parts as P  # noqa: E402
from sim.vehicle import load_specs  # noqa: E402


def test_stock_unchanged_and_parts_raise_power():
    specs = load_specs()
    base = specs["car"]
    assert P.apply_parts(base, {}, "auto") == base
    tuned = P.apply_parts(base, {"turbo": 2, "ecu": 3}, "auto")
    assert tuned["power_hp"] > base["power_hp"] and tuned["torque_nm"] > base["torque_nm"]
    assert tuned["redline_rpm"] == base["redline_rpm"] + 500
    assert base["power_hp"] == load_specs()["car"]["power_hp"]          # 원본은 그대로


def test_parts_only_for_matching_powertrain():
    base = load_specs()["car"]
    assert P.apply_parts(base, {"motor": 3}, "auto") == base             # 엔진차에 모터 파츠는 효과 없음
    ev = P.apply_parts(base, {"motor": 3, "battery": 2, "turbo": 3}, "ev")
    assert ev["ev"]["motor_power_kw"] > base["ev"]["motor_power_kw"]
    assert ev["mass"] > base["mass"] and ev["power_hp"] == base["power_hp"]
    assert {p[0] for p in P.applicable("hybrid")} == {p[0] for p in P.PARTS}


def test_performance_improves_with_race_parts():
    base = load_specs()["car"]
    full = {p[0]: 3 for p in P.PARTS}
    a = P.performance(base, "car", "auto")
    b = P.performance(P.apply_parts(base, full, "auto"), "car", "auto")
    assert 150 < a["top_kmh"] < b["top_kmh"]
    assert a["t100"] is not None and b["t100"] <= a["t100"] and b["t200"] < a["t200"]


def test_save_and_load(tmp_path):
    path = tmp_path / "garage.json"
    P.save_parts({"car": {"turbo": 2, "nope": 1}}, path)
    assert P.load_parts(path) == {"car": {"turbo": 2}}
    assert P.load_parts(tmp_path / "missing.json") == {}


def test_look_tier_follows_parts():
    assert P.tier({}, "auto") == 0
    assert P.tier({"turbo": 1}, "auto") == 1
    assert P.tier({p[0]: 3 for p in P.PARTS}, "auto") == 3
    assert P.tier({"turbo": 3}, "ev") == 0                     # 엔진 파츠는 전기차 외관에 영향 없음


def test_tires_raise_grip_for_every_powertrain():
    base = load_specs()["car"]
    for pt in ("auto", "manual", "ev", "hybrid"):
        assert P.apply_parts(base, {"tires": 3}, pt)["tire_mu"] > base["tire_mu"]
    a = P.performance(base, "car", "auto")
    b = P.performance(P.apply_parts(base, {"tires": 3}, "auto"), "car", "auto")
    assert b["t100"] < a["t100"]                              # 접지가 좋아지면 출발 가속이 빨라짐


def test_fan_kit_reaches_about_1_5s_on_ev():
    base = load_specs()["car"]
    assert P.apply_parts(base, {"fan": 3}, "auto") == base               # 엔진차에는 달 수 없음
    full = P.apply_parts(base, {"motor": 3, "inverter": 3, "tires": 3, "fan": 3}, "ev")
    perf = P.performance(full, "car", "ev")
    assert perf["t100"] is not None and perf["t100"] <= 1.6


def test_engine_sound_is_continuous_between_chunks():
    import numpy as np
    from viz import sound as S
    s = S.Sound.__new__(S.Sound)                    # 오디오 장치 없이 합성만
    s.n, s.ph, s.prev, s.rng = int(S.SR * S.CHUNK), {"eng": 0.0, "ev": 0.0, "turbo": 0.0}, None, np.random.default_rng(0)
    s._arr = lambda w: w
    tgt = dict(eng_vol=0.6, eng_f=30.0, load=0.0, rumble=0.2, ev_vol=0.0, ev_f=200.0, tb_vol=0.0, tb_f=900.0)
    w = []
    for i in range(40):                              # 공회전 → 레드라인까지 올리기
        tgt = dict(tgt, eng_f=27 + 5 * i, load=1.0)
        w.append(s._chunk(tgt))
    w = np.concatenate(w)
    assert np.abs(w).max() <= 1.0
    jumps = np.abs(np.diff(w))[s.n - 1::s.n]
    assert jumps.max() < np.percentile(np.abs(np.diff(w)), 99.9)   # 조각 경계에서 튀지 않음
