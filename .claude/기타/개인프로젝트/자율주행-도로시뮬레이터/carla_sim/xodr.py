"""긴 직선 도로(OpenDRIVE) 생성. CARLA 의 client.generate_opendrive_world() 로 바로 불러온다.

기본 30 km: 300 km/h 로 5분(25 km) 달리고도 가속·감속 구간이 남는 길이.
도로 제한속도는 400 km/h 로 적어 둔다 (차 쪽에는 속도 상한 없음, 300 km/h 는 측정 기준일 뿐).
"""


def _lane(lane_id, kind, width, mark):
    return (f'<lane id="{lane_id}" type="{kind}" level="false"><link/>'
            f'<width sOffset="0" a="{width}" b="0" c="0" d="0"/>'
            f'<roadMark sOffset="0" type="{mark}" weight="standard" color="white" width="0.15"/></lane>')


def long_straight_xodr(length=30000.0, lanes=3, lane_width=3.75):
    """한 방향 lanes 차로 + 양쪽 갓길의 직선 도로. 주행 차로 id 는 -1(왼쪽 끝) ~ -lanes(오른쪽 끝)."""
    right = "".join(_lane(-i, "driving", lane_width, "broken" if i < lanes else "solid")
                    for i in range(1, lanes + 1))
    right += _lane(-(lanes + 1), "shoulder", 2.5, "none")
    left = _lane(1, "shoulder", 1.0, "none")
    return f"""<?xml version="1.0" encoding="UTF-8"?>
<OpenDRIVE>
<header revMajor="1" revMinor="4" name="LongStraight" version="1.0"/>
<road name="LongStraight" length="{length:.1f}" id="0" junction="-1">
<link/>
<type s="0" type="motorway"><speed max="400" unit="km/h"/></type>
<planView><geometry s="0" x="0" y="0" hdg="0" length="{length:.1f}"><line/></geometry></planView>
<elevationProfile><elevation s="0" a="0" b="0" c="0" d="0"/></elevationProfile>
<lateralProfile/>
<lanes><laneSection s="0">
<left>{left}</left>
<center><lane id="0" type="none" level="false"><link/>
<roadMark sOffset="0" type="solid" weight="standard" color="yellow" width="0.15"/></lane></center>
<right>{right}</right>
</laneSection></lanes>
</road>
</OpenDRIVE>
"""
