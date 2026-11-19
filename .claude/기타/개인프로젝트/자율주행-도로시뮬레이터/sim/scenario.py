"""난이도(초급/중급/고급) 시나리오 설정.

자율주행 코드를 단계적으로 체험/검증하기 위한 묶음: 도로 형태, 트래픽 밀도, AI 운전 성향,
급제동 이벤트, 시야(안개).
"""

LEVELS = {
    "easy": dict(        # 초급: 완만한 타원, 차 적음, 양보 잘 하는 AI, 시야 좋음
        label="EASY", track="easy", n_cars=25, truck_ratio=0.08, bus_ratio=0.03,
        headway=(1.4, 2.0), politeness=0.5, lc_threshold=0.3, b_safe=2.0,
        brake_event_every=0.0, fog_dist=900.0,
    ),
    "normal": dict(      # 중급: S자 + 반경 200 m 곡선, 보통 밀도
        label="NORMAL", track="normal", n_cars=60, truck_ratio=0.12, bus_ratio=0.04,
        headway=(1.0, 1.6), politeness=0.3, lc_threshold=0.2, b_safe=3.0,
        brake_event_every=60.0, fog_dist=520.0,
    ),
    "hard": dict(        # 고급: 급곡선, 고밀도, 공격적 끼어들기, 앞차 급제동, 안개
        label="HARD", track="hard", n_cars=110, truck_ratio=0.2, bus_ratio=0.05,
        headway=(0.7, 1.2), politeness=0.0, lc_threshold=0.1, b_safe=4.0,
        brake_event_every=15.0, fog_dist=260.0,
    ),
}


def get_level(name):
    if name not in LEVELS:
        raise ValueError(f"난이도는 {list(LEVELS)} 중 하나: {name}")
    return dict(LEVELS[name], name=name)
