#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
업무프로세스 챗봇 및 로컬 LLM 통합 서비스 관리기 (manage_service.py)
- 평일(08:00~17:00) 자동 스케줄러 실행 및 공휴일 자동 스킵
- 수동 실행/종료 지원 및 중복 실행 방지
"""

import os
import sys
import time
import json
import subprocess
import urllib.request
import urllib.error
from datetime import datetime, date

# Windows 콘솔 인코딩 보정
if sys.platform == 'win32':
    try:
        sys.stdout.reconfigure(encoding='utf-8')
        sys.stderr.reconfigure(encoding='utf-8')
    except Exception:
        pass

BASE_DIR = r"D:\업무프로세스"
BOT_SCRIPT = os.path.join(BASE_DIR, "bot", "bot.py")
LOG_DIR = os.path.join(BASE_DIR, "logs")
SCHEDULER_LOG = os.path.join(LOG_DIR, "scheduler.log")
OLLAMA_EXE = os.path.expandvars(r"%LOCALAPPDATA%\Programs\Ollama\ollama.exe")
OLLAMA_APP = os.path.expandvars(r"%LOCALAPPDATA%\Programs\Ollama\ollama app.exe")

os.makedirs(LOG_DIR, exist_ok=True)

def log(msg):
    now_str = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{now_str}] {msg}"
    print(line)
    try:
        with open(SCHEDULER_LOG, "a", encoding="utf-8") as f:
            f.write(line + "\n")
    except Exception:
        pass

# 2025 ~ 2027 대한민국 법정 공휴일 (대체공휴일 포함)
HOLIDAYS = {
    # 2025년
    "2025-01-01": "신정",
    "2025-01-28": "설날 연휴", "2025-01-29": "설날", "2025-01-30": "설날 연휴",
    "2025-03-01": "삼일절", "2025-03-03": "대체공휴일",
    "2025-05-05": "어린이날", "2025-05-06": "부처님오신날",
    "2025-06-06": "현충일",
    "2025-08-15": "광복절",
    "2025-10-03": "개천절",
    "2025-10-05": "추석 연휴", "2025-10-06": "추석", "2025-10-07": "추석 연휴", "2025-10-08": "대체공휴일",
    "2025-10-09": "한글날",
    "2025-12-25": "성탄절",

    # 2026년
    "2026-01-01": "신정",
    "2026-02-16": "설날 연휴", "2026-02-17": "설날", "2026-02-18": "설날 연휴",
    "2026-03-01": "삼일절", "2026-03-02": "대체공휴일",
    "2026-05-05": "어린이날", "2026-05-24": "부처님오신날", "2026-05-25": "대체공휴일",
    "2026-06-06": "현충일",
    "2026-08-15": "광복절", "2026-08-17": "대체공휴일",
    "2026-09-24": "추석 연휴", "2026-09-25": "추석", "2026-09-26": "추석 연휴", "2026-09-28": "대체공휴일",
    "2026-10-03": "개천절", "2026-10-05": "대체공휴일",
    "2026-10-09": "한글날",
    "2026-12-25": "성탄절",

    # 2027년
    "2027-01-01": "신정",
    "2027-02-06": "설날 연휴", "2027-02-07": "설날", "2027-02-08": "설날 연휴", "2027-02-09": "대체공휴일",
    "2027-03-01": "삼일절",
    "2027-05-05": "어린이날", "2027-05-13": "부처님오신날",
    "2027-06-06": "현충일", "2027-06-07": "대체공휴일",
    "2027-08-15": "광복절", "2027-08-16": "대체공휴일",
    "2027-09-14": "추석 연휴", "2027-09-15": "추석", "2027-09-16": "추석 연휴",
    "2027-10-03": "개천절", "2027-10-04": "대체공휴일",
    "2027-10-09": "한글날", "2027-10-11": "대체공휴일",
    "2027-12-25": "성탄절",
}

def is_holiday_today():
    today = date.today()
    # 1. 주말 체크 (월=0, ... 금=4, 토=5, 일=6)
    if today.weekday() >= 5:
        day_name = "토요일" if today.weekday() == 5 else "일요일"
        return True, f"주말({day_name})"
    
    # 2. 법정 공휴일 체크
    date_str = today.strftime("%Y-%m-%d")
    if date_str in HOLIDAYS:
        return True, f"법정 공휴일({HOLIDAYS[date_str]})"
    
    # 3. 매년 고정 양력 공휴일 체크 (fallback)
    mm_dd = today.strftime("%m-%d")
    fixed_holidays = {
        "01-01": "신정", "03-01": "삼일절", "05-05": "어린이날",
        "06-06": "현충일", "08-15": "광복절", "10-03": "개천절",
        "10-09": "한글날", "12-25": "성탄절"
    }
    if mm_dd in fixed_holidays:
        return True, f"법정 공휴일({fixed_holidays[mm_dd]})"

    return False, "평일 정상 근무일"

def check_ollama_running():
    try:
        req = urllib.request.Request("http://127.0.0.1:11434/", method="GET")
        with urllib.request.urlopen(req, timeout=3) as resp:
            return resp.status == 200
    except Exception:
        return False

def start_ollama():
    if check_ollama_running():
        log("[LLM] Ollama가 이미 정상 실행 중입니다.")
        return True

    log("[LLM] Ollama 서비스 시작 중...")
    if os.path.exists(OLLAMA_APP):
        subprocess.Popen([OLLAMA_APP], shell=False)
    elif os.path.exists(OLLAMA_EXE):
        subprocess.Popen([OLLAMA_EXE, "serve"], shell=False, creationflags=subprocess.CREATE_NO_WINDOW if sys.platform == 'win32' else 0)
    else:
        subprocess.Popen(["ollama", "serve"], shell=True)

    # 포트 11434 응답 대기 (최대 20초)
    for _ in range(20):
        time.sleep(1)
        if check_ollama_running():
            log("[LLM] Ollama 준비 완료 (포트 11434 응답 확인)")
            return True

    log("[LLM 경고] Ollama 시작 후 응답 대기 시간 초과")
    return False

def stop_ollama():
    log("[LLM] Ollama 및 VRAM 해제/종료 진행...")
    # 1. 모델 언로드
    try:
        exe = OLLAMA_EXE if os.path.exists(OLLAMA_EXE) else "ollama"
        subprocess.run([exe, "stop", "qwen3:8b"], capture_output=True, check=False)
    except Exception:
        pass

    # 2. 프로세스 종료
    subprocess.run(["taskkill", "/F", "/IM", "ollama.exe", "/T"], capture_output=True, check=False)
    subprocess.run(["taskkill", "/F", "/IM", "ollama app.exe", "/T"], capture_output=True, check=False)
    log("[LLM] Ollama 프로세스 종료 완료")

def get_running_bot_pids():
    pids = []
    try:
        cmd = 'powershell -NoProfile -Command "Get-CimInstance Win32_Process | Where-Object { ($_.Name -like \'python*\' -or $_.Name -eq \'py.exe\') -and $_.CommandLine -like \'*bot.py*\' -and $_.CommandLine -notlike \'*manage_service*\' } | Select-Object -ExpandProperty ProcessId"'
        out = subprocess.check_output(cmd, shell=True, text=True).strip()
        for line in out.splitlines():
            line = line.strip()
            if line.isdigit() and int(line) != os.getpid():
                pids.append(int(line))
    except Exception:
        pass
    return pids

def stop_bot():
    pids = get_running_bot_pids()
    if not pids:
        log("[챗봇] 실행 중인 챗봇 프로세스가 없습니다.")
        return
    log(f"[챗봇] 기존 챗봇 프로세스 종료 중 (PIDs: {pids})...")
    for pid in pids:
        subprocess.run(["taskkill", "/F", "/PID", str(pid), "/T"], capture_output=True, check=False)
    log("[챗봇] 기존 챗봇 프로세스 종료 완료")

def start_bot(visible_window=True):
    # LLM 먼저 준비
    start_ollama()

    # 이미 실행 중인지 확인
    existing_pids = get_running_bot_pids()
    if existing_pids:
        log(f"[챗봇] 챗봇이 이미 실행 중입니다 (PIDs: {existing_pids}). 새로 띄우지 않고 기존 서비스를 유지합니다.")
        return

    log("[챗봇] 챗봇 프로세스 시작...")
    py_launcher = r"C:\Users\qazx2\AppData\Local\Programs\Python\Launcher\py.exe"
    py_cmd = py_launcher if os.path.exists(py_launcher) else "py"

    if visible_window:
        # 사용자가 바탕화면에서 클릭했을 때: 콘솔 창을 띄워서 대화 상황을 직접 볼 수 있게 함
        bat_path = os.path.join(BASE_DIR, "run.bat")
        subprocess.Popen(f'start "업무프로세스 ntfy 챗봇" cmd /k "{bat_path}"', shell=True)
    else:
        # 스케줄러에 의한 백그라운드 자동 기동
        subprocess.Popen([py_cmd, "-u", BOT_SCRIPT], cwd=BASE_DIR, shell=False, creationflags=subprocess.CREATE_NEW_CONSOLE if sys.platform == 'win32' else 0)

    log("[챗봇] 챗봇 기동 명령 전송 완료")

def run_scheduler_start():
    log("=" * 50)
    log("[스케줄러 시작] 평일 08:00 정기 기동 검사 시작")
    holiday, reason = is_holiday_today()
    if holiday:
        log(f"[스케줄러 스킵] 오늘은 {reason}입니다. 챗봇을 실행하지 않습니다.")
        log("=" * 50)
        return

    log(f"[스케줄러 실행] 오늘은 {reason}입니다. 챗봇 및 LLM 서비스를 가동합니다.")
    start_bot(visible_window=False)
    log("=" * 50)

def run_scheduler_stop():
    log("=" * 50)
    log("[스케줄러 종료] 평일 17:00 정기 업무 종료 프로세스 시작")
    stop_bot()
    stop_ollama()
    log("[스케줄러 종료 완료] 챗봇 및 LLM(VRAM 해제) 정상 종료 완료")
    log("=" * 50)

if __name__ == "__main__":
    action = sys.argv[1].lower() if len(sys.argv) > 1 else "status"

    if action == "start-llm":
        start_ollama()
    elif action == "stop-llm":
        stop_ollama()
    elif action == "start-bot":
        start_bot(visible_window=True)
    elif action == "stop-bot":
        stop_bot()
    elif action == "scheduler-start":
        run_scheduler_start()
    elif action == "scheduler-stop":
        run_scheduler_stop()
    elif action == "status":
        h, r = is_holiday_today()
        print(f"오늘 상태: {r} (공휴일 여부: {h})")
        print(f"Ollama 실행 상태: {'실행 중' if check_ollama_running() else '중지됨'}")
        pids = get_running_bot_pids()
        print(f"챗봇 실행 상태: {'실행 중 (PIDs: ' + str(pids) + ')' if pids else '중지됨'}")
    else:
        print(f"알 수 없는 명령: {action}")
