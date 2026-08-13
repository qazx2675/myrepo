# 업무시트 엑셀 수식 모음 (COUNTIFS / SUMPRODUCT)

## 월별 재설치 건수 카운트 (COUNTIFS)
```
=COUNTIFS('2026년 업무시트'!$B:$B, $B$37, '2026년 업무시트'!$E:$E, "*재설치*",
 '2026년 업무시트'!$A:$A, ">="&DATE(2026, SUBSTITUTE($B39, "월", ""), 1))
```

## 월별/조건별 집계 (SUMPRODUCT, 최종본)
```
=SUMPRODUCT(
  (A시트!$B$2:$B$1000=$B$2) *
  (MONTH(A시트!$A$2:$A$1000 + 0) = VALUE(SUBSTITUTE($B4, "월", ""))) *
  (A시트!$D$2:$D$1000 = "asdf") *
  IFERROR(MID(A시트!$E$2:$E$1000, SEARCH(...)), 0)
)
```

## SUMPRODUCT 이전 버전 (단순 조건 3개)
```
=SUMPRODUCT(
  (A시트!$B$2:$B$1000=$B$2) *
  (MONTH(A시트!$A$2:$A$1000 + 0) = VALUE(SUBSTITUTE($B4, "월", ""))) *
  (A시트!$D$2:$D$1000 = "asdf")
)
```
