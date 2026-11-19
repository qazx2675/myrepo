"""절차적 사운드 (음원 파일 없이 numpy 로 생성).

- 엔진·전기모터·터보 휘슬은 매 프레임 짧은 조각(CHUNK)을 실시간으로 합성해 이어 붙인다.
  위상과 음높이·볼륨이 조각 사이에서 끊기지 않고 이어져서, RPM 이 바뀔 때 계단식으로 튀거나
  딸깍거리지 않는다.
- 엔진음: 폭발(배기 펄스) 주파수의 배음 합. 가속 페달 부하가 클수록 높은 배음이 살아나 밝아지고,
  0.5·1.5 차 배음(실린더마다 조금씩 다른 폭발)이 "부르릉" 하는 질감, 폭발에 맞춘 연소 잡음이 거친 맛을 낸다.
- 전기모터음: 모터 회전수에 비례하는 고음 윙~
- 타이어 스키드음: 미끄러짐 정도에 비례 / 풍절음: 속도 제곱에 비례
- 튜닝 파츠(ego.parts): 배기 = 엔진음 크고 저음 부르릉 강하게, 흡기 = 흡입음, 터보 = 휘슬 + 페달 뗄 때
  블로오프, 전기 모터 = 모터음 크고 높게
오디오 장치가 없으면 조용히 꺼진다.
"""

import numpy as np

SR = 22050
CHUNK = 0.06                                  # 실시간 합성 조각 길이(초). 2조각까지 미리 넣어 둔다.
# (배음 차수, 세기) — 0.5·1.5 차는 배기 튜닝에 따라 세짐
ORDERS = ((0.5, 0.0), (1, 1.0), (1.5, 0.0), (2, 0.55), (3, 0.35), (4, 0.22), (5, 0.12), (6, 0.08), (8, 0.04))


def _noise(dur, smooth, seed):
    n = int(SR * dur)
    x = np.random.default_rng(seed).standard_normal(n)
    k = np.ones(smooth) / smooth
    x = np.convolve(np.concatenate([x[-smooth:], x]), k, mode="same")[smooth:]
    return x


class Sound:
    def __init__(self):
        self.ok = False
        self.enabled = True
        try:
            import pygame
            pygame.mixer.pre_init(SR, -16, 1, 512)
            pygame.mixer.init(SR, -16, 1, 512)
            if pygame.mixer.get_init() is None:
                return
            self.pg = pygame
            pygame.mixer.set_num_channels(8)
        except Exception:
            return
        try:
            self._build()
        except Exception:          # 소리 생성 실패는 시뮬레이터를 멈추지 않는다
            self.ok = False

    def _build(self):
        pygame = self.pg
        mk = self._mk
        tire = _noise(1.0, 3, 2) + 0.5 * np.sin(2 * np.pi * 1300 * np.arange(SR) / SR)
        self.tire = mk(tire)
        self.wind = mk(_noise(2.0, 25, 3))
        n = int(SR * 0.45)
        self.bov = mk(_noise(0.45, 2, 5) * np.exp(-np.arange(n) / (SR * 0.12)))   # 블로오프 "치익"
        self.intake = mk(_noise(1.5, 6, 7))
        names = ("stream", "tire", "wind", "bov", "intake")
        self.ch = {k: pygame.mixer.Channel(i) for i, k in enumerate(names)}
        self.n = int(SR * CHUNK)
        self.ph = {"eng": 0.0, "ev": 0.0, "turbo": 0.0}       # 발진기 위상 (주기 단위)
        self.prev = None                                      # 지난 조각 끝의 목표값
        self.rng = np.random.default_rng(4)
        self._thr = 0.0
        self.ch["tire"].play(self.tire, loops=-1)
        self.ch["wind"].play(self.wind, loops=-1)
        self.ch["intake"].play(self.intake, loops=-1)
        for c in self.ch.values():
            c.set_volume(0.0)
        self.ok = True

    def _arr(self, w):
        a = (np.clip(w, -1.0, 1.0) * 32767).astype(np.int16)
        ch = self.pg.mixer.get_init()[2]          # pygame.init() 이 이미 스테레오로 열었을 수 있음
        if ch > 1:
            a = np.ascontiguousarray(np.repeat(a[:, None], ch, axis=1))
        return self.pg.sndarray.make_sound(a)

    def _mk(self, w):
        return self._arr(w / (np.abs(w).max() + 1e-9) * 0.6)

    def _osc(self, name, f):
        """위상이 이어지는 발진기: 주파수 배열 f(Hz) → 위상 배열(주기). 0.5 차 배음 때문에 2주기마다 되돌린다."""
        ph = self.ph[name] + np.cumsum(f) / SR
        self.ph[name] = float(ph[-1] % 2.0)
        return ph

    def _chunk(self, tgt):
        """목표값(지난 조각 끝 → 이번 목표로 선형 보간)으로 CHUNK 길이 소리 합성."""
        p = self.prev or tgt
        self.prev = tgt
        r = np.arange(1, self.n + 1) / self.n
        lerp = lambda k: p[k] + (tgt[k] - p[k]) * r   # noqa: E731
        out = np.zeros(self.n)
        vol = lerp("eng_vol")
        if vol.max() > 1e-4:
            f, load, rumble = lerp("eng_f"), lerp("load"), lerp("rumble")
            ph = self._osc("eng", f)
            cut = 350 + 1100 * load                       # 저역 통과(배기관 공명) — 부하가 크면 밝아짐
            w = np.zeros(self.n)
            for h, a in ORDERS:
                amp = a if a else rumble
                w += amp / (1 + (f * h / cut) ** 2) * np.sin(2 * np.pi * h * ph)
            pulse = (0.5 + 0.5 * np.cos(2 * np.pi * ph)) ** 6   # 폭발 순간에만 연소 잡음
            w += (0.12 + 0.35 * load) * pulse * self.rng.standard_normal(self.n)
            out += vol * 0.38 * w
        vol = lerp("ev_vol")
        if vol.max() > 1e-4:
            ph = self._osc("ev", lerp("ev_f"))
            out += vol * (np.sin(2 * np.pi * ph) + 0.3 * np.sin(4 * np.pi * ph)) * 0.45
        vol = lerp("tb_vol")
        if vol.max() > 1e-4:
            ph = self._osc("turbo", lerp("tb_f"))
            out += vol * (np.sin(2 * np.pi * ph) + 0.15 * np.sin(6 * np.pi * ph)) * 0.5
        return self._arr(out)

    def _stream(self, tgt):
        """재생 중 1조각 + 대기 1조각을 유지 (모자라면 채운다)."""
        ch = self.ch["stream"]
        ch.set_volume(1.0)
        for _ in range(2):
            if not ch.get_busy():
                ch.play(self._chunk(tgt))
            elif ch.get_queue() is None:
                ch.queue(self._chunk(tgt))

    def update(self, ego, throttle):
        if not self.ok:
            return
        if not self.enabled:
            for c in self.ch.values():
                c.set_volume(0.0)
            return
        v = abs(ego.vx)
        parts = getattr(ego, "parts", None) or {}
        ex, tb, ik, mo = (parts.get(k, 0) for k in ("exhaust", "turbo", "intake", "motor"))
        tgt = dict(eng_vol=0.0, eng_f=30.0, load=0.0, rumble=0.0, ev_vol=0.0, ev_f=200.0, tb_vol=0.0, tb_f=900.0)
        if self.prev:                               # 꺼진 소리도 음높이는 그대로 두고 볼륨만 줄인다
            tgt.update(eng_f=self.prev["eng_f"], ev_f=self.prev["ev_f"], tb_f=self.prev["tb_f"])
        engine_running = ego.powertrain != "ev" and getattr(ego, "engine_on", True)
        if engine_running:
            tgt["eng_f"] = max(ego.rpm, 1.0) / 60.0 * 2.0    # 4기통: 회전당 2번 폭발
            tgt["load"] = min(1.0, throttle * (1 + 0.15 * ex))
            tgt["rumble"] = 0.18 + 0.1 * ex                   # 배기 튜닝일수록 부르릉
            tgt["eng_vol"] = min(1.0, (0.22 + 0.35 * throttle) * (1 + 0.22 * ex))
            spool = min(1.0, ego.rpm / getattr(ego, "redline", 6500)) * throttle
            if tb:
                tgt["tb_f"], tgt["tb_vol"] = 900 + 5500 * spool, 0.05 * tb * spool
                if self._thr > 0.6 and throttle < 0.1 and ego.rpm > 2500:
                    self.ch["bov"].play(self.bov)
                    self.ch["bov"].set_volume(0.12 * tb)
            self.ch["intake"].set_volume(0.06 * ik * throttle)
        else:
            self.ch["intake"].set_volume(0.0)
        self._thr = throttle
        if ego.powertrain in ("ev", "hybrid"):
            tgt["ev_f"] = (150 + v * 45) * (1 + 0.12 * mo)
            tgt["ev_vol"] = min(0.4, (0.02 + abs(ego.power_kw) / 800 + v / 400) * (1 + 0.35 * mo))
        self._stream(tgt)
        slip = 0.0
        if ego.sliding or ego.abs_active or ego.wheelspin:
            slip = 0.5
        if v > 3:
            slip = max(slip, min(1.0, abs(ego.vy) / 2.0))
        self.ch["tire"].set_volume(0.5 * slip)
        self.ch["wind"].set_volume(min(0.5, (v / 45.0) ** 2))
