(function() {
    // 기본 제외 호스트 패턴 (실제 환경 소스 진행 시 자동 추가되는 6~7대 필수 서버를 미리 지정 가능)
    const DEFAULT_EXCLUDE_HOSTS = "prd-db-0295|prd-db-0296|prd-db-0297|prd-db-0298|prd-db-0299|prd-db-0300";

    // 기존 패널이 열려 있다면 안전하게 제거
    const prevPanel = document.getElementById('awx-validator-panel');
    if (prevPanel) prevPanel.remove();

    // 상태 저장 객체
    window.awxValidatorState = window.awxValidatorState || {
        collectedHosts: new Set(),
        excludedHosts: [],
        rawCount: 0,
        excludePattern: DEFAULT_EXCLUDE_HOSTS,
        detailText: '',
        incidentText: '',
        targetText: '',
        isStandalone: false
    };

    const state = window.awxValidatorState;

    // 현재 접속 중인 AWX 화면 컨텍스트 분석 (인벤토리 ID, 그룹 ID, 또는 전역)
    function getContext() {
        const hash = window.location.hash || '';
        const invMatch = hash.match(/\/inventories\/(?:inventory|smart_inventory)\/(\d+)/);
        if (invMatch) {
            return {
                url: `/api/v2/inventories/${invMatch[1]}/hosts/?page_size=200`,
                label: `인벤토리 #${invMatch[1]}`,
                isScoped: true
            };
        }
        const grpMatch = hash.match(/\/groups\/(\d+)\/(?:nested_hosts|hosts)/);
        if (grpMatch) {
            return {
                url: `/api/v2/groups/${grpMatch[1]}/all_hosts/?page_size=200`,
                label: `그룹 #${grpMatch[1]}`,
                isScoped: true
            };
        }
        return {
            url: '/api/v2/hosts/?page_size=200',
            label: '전체 인벤토리 전역',
            isScoped: false
        };
    }

    // 제외 패턴 문자열 파싱 (정확 일치 및 부분 포함 매칭)
    function getExcludeFilters(patternStr) {
        if (!patternStr || !patternStr.trim()) return [];
        return patternStr.split('|')
            .map(s => s.trim().toLowerCase())
            .filter(Boolean);
    }

    function isHostExcluded(host, filters) {
        if (!filters || filters.length === 0) return false;
        const h = host.toLowerCase();
        return filters.some(f => h === f || h.includes(f));
    }

    // 텍스트 파서 (줄바꿈, 공백, 콤마, 세미콜론, 탭 분리 및 시리얼 제거)
    function parseHostText(rawText) {
        if (!rawText) return [];
        const normalized = rawText.replace(/\\n/g, '\n').trim();
        const resultSet = new Set();
        const lines = normalized.split(/[\r\n]+/);
        lines.forEach(line => {
            const segments = line.replace(/[;,\t]+/g, ' ').split(/\s+/);
            segments.forEach(seg => {
                let clean = seg.trim();
                if (!clean) return;
                clean = clean.split('/')[0].split('(')[0].trim().toLowerCase();
                if (clean && clean !== '/' && clean !== '-' && !clean.startsWith('sn:') && !clean.startsWith('sn=')) {
                    resultSet.add(clean);
                }
            });
        });
        return Array.from(resultSet);
    }

    // HTML UI 생성 함수 (패널 또는 독립 창 공용)
    function buildUIHTML(ctx) {
        return `
        <div id="awx-vld-header" style="background:#0f172a; color:#f8fafc; padding:10px 14px; font-weight:700; display:flex; justify-content:space-between; align-items:center; cursor:move; user-select:none; border-bottom:1px solid #1e293b;">
            <div style="display:flex; align-items:center; gap:8px;">
                <span style="font-size:13px; letter-spacing:-0.3px;">작업대상 호스트 교차 검증기</span>
                <span id="awx-vld-ctx-badge" style="background:#1e293b; color:#94a3b8; font-size:11px; padding:2px 6px; border-radius:3px; font-weight:500;">${ctx.label}</span>
            </div>
            <div style="display:flex; align-items:center; gap:6px;">
                <button id="awx-vld-btn-detach" style="background:#1e293b; color:#cbd5e1; border:1px solid #334155; padding:3px 8px; border-radius:3px; font-size:11px; cursor:pointer;" title="패널 크기 맞춤 독립 브라우저 팝업 창으로 띄우기">독립 창으로 분리</button>
                <button id="awx-vld-btn-close" style="background:none; border:none; color:#94a3b8; font-size:16px; cursor:pointer; line-height:1; padding:0 4px;">&times;</button>
            </div>
        </div>

        <div style="padding:14px; overflow-y:auto; flex:1; display:flex; flex-direction:column; gap:12px; background:#f8fafc;">
            <!-- 1. AWX 호스트 수집 및 제외 섹션 -->
            <div style="background:#ffffff; border:1px solid #e2e8f0; border-radius:4px; padding:12px;">
                <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:8px;">
                    <span style="font-weight:700; font-size:12px; color:#0f172a;">1. AWX 호스트 수집 (선택 - 1대 이상 시 비교 참여)</span>
                    <button id="awx-vld-btn-reset-awx" style="background:#f1f5f9; color:#475569; border:1px solid #cbd5e1; padding:2px 6px; border-radius:3px; cursor:pointer; font-size:11px;">수집 초기화</button>
                </div>

                <!-- 제외 패턴 입력란 -->
                <div style="margin-bottom:8px;">
                    <label style="display:block; font-size:11px; font-weight:600; color:#334155; margin-bottom:3px;">제외 호스트 설정 (| 구분)</label>
                    <input id="awx-vld-input-exclude" type="text" value="${state.excludePattern}" placeholder="예: host01|host02|bastion" style="width:100%; box-sizing:border-box; border:1px solid #cbd5e1; border-radius:3px; padding:5px 8px; font-size:11px; font-family:monospace; color:#0f172a; background:#ffffff;">
                    <div id="awx-vld-exclude-hint" style="font-size:10px; color:#64748b; margin-top:3px; ${state.excludePattern ? 'display:none;' : ''}">OS설치에 필요한 서버명은 이미 제외되어 있음</div>
                </div>

                <!-- 수집 액션 버튼 -->
                <div style="display:flex; gap:6px; margin-bottom:8px;">
                    <button id="awx-vld-btn-page" style="flex:1; background:#ffffff; color:#0f172a; border:1px solid #cbd5e1; padding:6px; border-radius:3px; font-size:11px; font-weight:600; cursor:pointer;">현재 화면 수집</button>
                    <button id="awx-vld-btn-collect-all" style="flex:1; background:#0f172a; color:#ffffff; border:1px solid #0f172a; padding:6px; border-radius:3px; font-size:11px; font-weight:600; cursor:pointer;">현재 인벤토리 전체 수집</button>
                </div>

                <!-- AWX 수집 결과 상태 및 제외 요약 -->
                <div id="awx-vld-awx-status" style="font-size:11px; line-height:1.5; color:#334155; background:#f1f5f9; padding:8px; border-radius:3px; border:1px solid #e2e8f0;">
                    현재 수집된 호스트: <b id="awx-vld-count-awx" style="color:#0f172a;">${state.collectedHosts.size}</b> 대
                    <div id="awx-vld-awx-summary" style="margin-top:4px;"></div>
                </div>
            </div>

            <!-- 2. 비교 대상 텍스트 입력창 (상세목록현황, 인시던트, 작업대상) -->
            <div style="background:#ffffff; border:1px solid #e2e8f0; border-radius:4px; padding:12px; display:flex; flex-direction:column; gap:10px;">
                <div style="font-weight:700; font-size:12px; color:#0f172a;">2. 비교 대상 호스트 입력</div>

                <!-- 입력 1: 상세목록현황 [필수] -->
                <div>
                    <div style="display:flex; justify-content:space-between; margin-bottom:3px;">
                        <span style="font-size:11px; font-weight:700; color:#0f172a;">상세목록현황 호스트 <span style="color:#dc2626;">[필수]</span></span>
                        <span style="font-size:10px; color:#64748b;">인식: <b id="awx-vld-cnt-detail" style="color:#0f172a;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-detail" placeholder="상세목록현황 호스트 붙여넣기 (개행/공백/콤마/탭/시리얼 자동 분리)" style="width:100%; height:64px; box-sizing:border-box; border:1px solid #cbd5e1; border-radius:3px; padding:6px; font-size:11px; font-family:monospace; resize:vertical;">${state.detailText}</textarea>
                </div>

                <!-- 입력 2: 인시던트 [필수] -->
                <div>
                    <div style="display:flex; justify-content:space-between; margin-bottom:3px;">
                        <span style="font-size:11px; font-weight:700; color:#0f172a;">인시던트 호스트 <span style="color:#dc2626;">[필수]</span></span>
                        <span style="font-size:10px; color:#64748b;">인식: <b id="awx-vld-cnt-incident" style="color:#0f172a;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-incident" placeholder="인시던트 작업대상 호스트 붙여넣기 (개행/공백/콤마/탭/시리얼 자동 분리)" style="width:100%; height:64px; box-sizing:border-box; border:1px solid #cbd5e1; border-radius:3px; padding:6px; font-size:11px; font-family:monospace; resize:vertical;">${state.incidentText}</textarea>
                </div>

                <!-- 입력 3: 작업대상 [선택] -->
                <div>
                    <div style="display:flex; justify-content:space-between; margin-bottom:3px;">
                        <span style="font-size:11px; font-weight:600; color:#334155;">작업대상 호스트 <span style="color:#64748b; font-weight:normal;">[선택 - 1대 이상 시 비교 참여]</span></span>
                        <span style="font-size:10px; color:#64748b;">인식: <b id="awx-vld-cnt-target" style="color:#0f172a;">0</b>대</span>
                    </div>
                    <textarea id="awx-vld-txt-target" placeholder="작업대상 호스트 붙여넣기 (선택사항, 비어있으면 비교 제외)" style="width:100%; height:54px; box-sizing:border-box; border:1px solid #cbd5e1; border-radius:3px; padding:6px; font-size:11px; font-family:monospace; resize:vertical;">${state.targetText}</textarea>
                </div>
            </div>

            <!-- 3. 비교 실행 버튼 -->
            <button id="awx-vld-btn-compare" style="width:100%; background:#1d4ed8; color:#ffffff; border:none; padding:10px; border-radius:4px; font-size:13px; font-weight:700; cursor:pointer; letter-spacing:-0.2px;">
                교차 비교 실행 (2개 소스)
            </button>

            <!-- 4. 검증 결과 리포트 영역 -->
            <div id="awx-vld-result-box" style="display:none; background:#ffffff; border:1px solid #cbd5e1; border-radius:4px; padding:12px; font-size:12px; line-height:1.5;"></div>

            <!-- 5. 고정 완료 권장 코멘트 (결과 표시 여부와 무관하게 상시 확인 가능하도록 하단 배치) -->
            <div style="background:#f1f5f9; border:1px solid #cbd5e1; border-radius:4px; padding:8px 10px; font-size:11px; color:#0f172a; line-height:1.4;">
                <div style="font-weight:700; color:#475569; margin-bottom:2px; font-size:10px;">작업 권장 안내</div>
                <div style="font-family:Consolas, monospace; word-break:break-all;">
                    인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -&gt; esc -&gt; :set paste 입력(붙여넣기 모드) -&gt; 붙여넣기
                </div>
            </div>
        </div>
        `;
    }

    // 메인 패널 엘리먼트 생성
    const panel = document.createElement('div');
    panel.id = 'awx-validator-panel';
    panel.style.cssText = `
        position: fixed; top: 60px; right: 20px; width: 480px; max-height: 88vh;
        background: #ffffff; border: 1px solid #0f172a; border-radius: 6px;
        box-shadow: 0 10px 25px rgba(15, 23, 42, 0.25); z-index: 9999999;
        font-family: -apple-system, BlinkMacSystemFont, "Pretendard", "Segoe UI", Roboto, sans-serif;
        font-size: 12px; color: #0f172a; display: flex; flex-direction: column; overflow: hidden;
    `;

    const ctx = getContext();
    panel.innerHTML = buildUIHTML(ctx);
    document.body.appendChild(panel);

    // 이벤트 및 로직 바인딩 함수
    function bindEvents(rootDoc, isPopup = false, popupWin = null) {
        const getEl = id => rootDoc.getElementById(id);

        const headerEl = getEl('awx-vld-header');
        const btnClose = getEl('awx-vld-btn-close');
        const btnDetach = getEl('awx-vld-btn-detach');
        const inputExclude = getEl('awx-vld-input-exclude');
        const hintExclude = getEl('awx-vld-exclude-hint');
        const btnResetAwx = getEl('awx-vld-btn-reset-awx');
        const btnPage = getEl('awx-vld-btn-page');
        const btnCollectAll = getEl('awx-vld-btn-collect-all');
        const countAwxEl = getEl('awx-vld-count-awx');
        const awxSummaryEl = getEl('awx-vld-awx-summary');
        const txtDetail = getEl('awx-vld-txt-detail');
        const txtIncident = getEl('awx-vld-txt-incident');
        const txtTarget = getEl('awx-vld-txt-target');
        const cntDetail = getEl('awx-vld-cnt-detail');
        const cntIncident = getEl('awx-vld-cnt-incident');
        const cntTarget = getEl('awx-vld-cnt-target');
        const btnCompare = getEl('awx-vld-btn-compare');
        const resultBox = getEl('awx-vld-result-box');

        // 드래그 기능 (페이지 내 패널일 때만 적용)
        if (!isPopup && headerEl) {
            let isDragging = false;
            let startX, startY, initialLeft, initialTop;
            headerEl.addEventListener('mousedown', (e) => {
                if (e.target.tagName === 'BUTTON') return;
                isDragging = true;
                startX = e.clientX;
                startY = e.clientY;
                const rect = panel.getBoundingClientRect();
                initialLeft = rect.left;
                initialTop = rect.top;
                panel.style.right = 'auto';
                panel.style.left = initialLeft + 'px';
                panel.style.top = initialTop + 'px';
            });
            document.addEventListener('mousemove', (e) => {
                if (!isDragging) return;
                const dx = e.clientX - startX;
                const dy = e.clientY - startY;
                panel.style.left = Math.max(10, Math.min(window.innerWidth - panel.offsetWidth - 10, initialLeft + dx)) + 'px';
                panel.style.top = Math.max(10, Math.min(window.innerHeight - panel.offsetHeight - 10, initialTop + dy)) + 'px';
            });
            document.addEventListener('mouseup', () => { isDragging = false; });
        }

        // 닫기 버튼
        btnClose.onclick = () => {
            if (isPopup && popupWin) {
                popupWin.close();
            } else {
                panel.style.display = 'none';
            }
        };

        // 독립 창 분리 / 복귀 버튼
        if (isPopup) {
            btnDetach.innerText = '페이지 내로 복귀';
            btnDetach.onclick = () => {
                if (popupWin) popupWin.close();
                panel.style.display = 'flex';
                updateCountsAndButton();
            };
        } else {
            btnDetach.onclick = () => {
                panel.style.display = 'none';
                const popup = window.open('', 'AWXHostValidatorPopup', 'width=540,height=820,menubar=no,toolbar=no,location=no,status=no,resizable=yes,scrollbars=yes');
                if (!popup) {
                    alert('팝업 차단이 감지되었습니다. 팝업 허용 후 다시 시도해주세요.');
                    panel.style.display = 'flex';
                    return;
                }
                popup.document.write(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>작업대상 호스트 교차 검증기</title></head><body style="margin:0; padding:0; background:#f8fafc; font-family:-apple-system, BlinkMacSystemFont, 'Pretendard', sans-serif;">${buildUIHTML(ctx)}</body></html>`);
                popup.document.close();
                bindEvents(popup.document, true, popup);
            };
        }

        // 제외 입력 안내 문구 토글
        inputExclude.oninput = () => {
            state.excludePattern = inputExclude.value.trim();
            if (state.excludePattern) {
                hintExclude.style.display = 'none';
            } else {
                hintExclude.style.display = 'block';
            }
        };

        // 텍스트 실시간 카운트 및 비교 버튼 라벨 갱신
        function updateCountsAndButton() {
            state.detailText = txtDetail.value;
            state.incidentText = txtIncident.value;
            state.targetText = txtTarget.value;

            const detailHosts = parseHostText(state.detailText);
            const incidentHosts = parseHostText(state.incidentText);
            const targetHosts = parseHostText(state.targetText);

            cntDetail.innerText = detailHosts.length;
            cntIncident.innerText = incidentHosts.length;
            cntTarget.innerText = targetHosts.length;
            countAwxEl.innerText = state.collectedHosts.size;

            let sourceCount = 0;
            if (detailHosts.length > 0) sourceCount++;
            if (incidentHosts.length > 0) sourceCount++;
            if (targetHosts.length > 0) sourceCount++;
            if (state.collectedHosts.size > 0) sourceCount++;

            btnCompare.innerText = `교차 비교 실행 (${Math.max(2, sourceCount)}개 소스)`;
        }

        txtDetail.oninput = updateCountsAndButton;
        txtIncident.oninput = updateCountsAndButton;
        txtTarget.oninput = updateCountsAndButton;

        // AWX 수집 초기화
        btnResetAwx.onclick = () => {
            state.collectedHosts.clear();
            state.excludedHosts = [];
            state.rawCount = 0;
            awxSummaryEl.innerHTML = '';
            updateCountsAndButton();
        };

        // 1) 화면 UI에서 현재 페이지 수집
        btnPage.onclick = () => {
            const filters = getExcludeFilters(inputExclude.value.trim());
            let pageRaw = 0;
            let pageExcluded = [];
            document.querySelectorAll('table tbody tr').forEach(tr => {
                const nameEl = tr.querySelector('td[data-label="이름"] a, td[data-label="Name"] a, td a[href*="/hosts/"]') || tr.querySelector('td:nth-child(2) a');
                if (nameEl) {
                    const h = nameEl.innerText.trim().toLowerCase();
                    if (h) {
                        pageRaw++;
                        if (isHostExcluded(h, filters)) {
                            pageExcluded.push(h);
                        } else {
                            state.collectedHosts.add(h);
                        }
                    }
                }
            });
            state.rawCount += pageRaw;
            state.excludedHosts.push(...pageExcluded);
            renderAwxSummary();
            updateCountsAndButton();
        };

        // 2) AWX API 전수 수집
        async function runCollectAll() {
            btnCollectAll.disabled = true;
            btnCollectAll.innerText = '수집 중...';
            const currentContext = getContext();
            const filters = getExcludeFilters(inputExclude.value.trim());

            state.collectedHosts.clear();
            state.excludedHosts = [];
            let totalRaw = 0;

            try {
                let nextUrl = currentContext.url;
                while (nextUrl) {
                    const res = await fetch(nextUrl);
                    const data = await res.json();
                    if (data.results) {
                        data.results.forEach(item => {
                            const name = item.name.trim().toLowerCase();
                            totalRaw++;
                            if (isHostExcluded(name, filters)) {
                                state.excludedHosts.push(name);
                            } else {
                                state.collectedHosts.add(name);
                            }
                        });
                    }
                    nextUrl = data.next;
                }
                state.rawCount = totalRaw;
                renderAwxSummary();
            } catch (err) {
                awxSummaryEl.innerHTML = `<span style="color:#dc2626;">수집 실패: ${err.message}</span>`;
            } finally {
                btnCollectAll.disabled = false;
                btnCollectAll.innerText = '현재 인벤토리 전체 수집';
                updateCountsAndButton();
            }
            return state.collectedHosts.size;
        }

        btnCollectAll.onclick = runCollectAll;
        window.awxRunCollectAll = runCollectAll;

        // AWX 수집 제외 요약 출력 렌더링
        function renderAwxSummary() {
            if (state.rawCount === 0) {
                awxSummaryEl.innerHTML = '';
                return;
            }
            const exCount = state.excludedHosts.length;
            if (exCount > 0) {
                awxSummaryEl.innerHTML = `
                    <div style="font-weight:700; color:#b45309; margin-top:4px;">
                        ${state.rawCount}대 중 ${exCount}대 제외됨 (수집 대상: ${state.collectedHosts.size}대)
                    </div>
                    <div style="font-family:monospace; font-size:10px; color:#475569; margin-top:2px; word-break:break-all;">
                        ${state.excludedHosts.join(' ')}
                    </div>
                `;
            } else {
                awxSummaryEl.innerHTML = `<div style="color:#0f172a; margin-top:2px;">${state.rawCount}대 수집 완료 (제외 0대)</div>`;
            }
        }

        // 3) 교차 비교 실행 함수
        function doCompare() {
            const detailList = parseHostText(txtDetail.value);
            const incidentList = parseHostText(txtIncident.value);
            const targetList = parseHostText(txtTarget.value);
            const awxList = Array.from(state.collectedHosts);

            // 필수 입력 검증
            if (detailList.length === 0 || incidentList.length === 0) {
                resultBox.style.display = 'block';
                resultBox.style.background = '#fef2f2';
                resultBox.style.borderColor = '#fca5a5';
                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; margin-bottom:4px;">오류: 필수 입력 항목 누락</div>
                    <div style="color:#991b1b; font-size:11px;">
                        상세목록현황과 인시던트 호스트는 필수 입력 대상입니다. 두 입력창에 호스트를 입력해주세요.
                    </div>
                `;
                return;
            }

            // 활성 소스 목록 구성
            const activeSources = [];
            activeSources.push({ id: 'detail', name: '상세목록현황', hosts: new Set(detailList) });
            activeSources.push({ id: 'incident', name: '인시던트', hosts: new Set(incidentList) });

            if (targetList.length > 0) {
                activeSources.push({ id: 'target', name: '작업대상', hosts: new Set(targetList) });
            }
            if (awxList.length > 0) {
                activeSources.push({ id: 'awx', name: 'AWX 수집 목록', hosts: new Set(awxList) });
            }

            // 전체 합집합
            const unionSet = new Set();
            activeSources.forEach(s => s.hosts.forEach(h => unionSet.add(h)));

            // 각 소스별 차이 분석
            let isAllMatch = true;
            const diffs = [];

            activeSources.forEach(source => {
                const missingInSource = []; // 전체 집합에는 있으나 이 소스에는 없는 호스트
                unionSet.forEach(h => {
                    if (!source.hosts.has(h)) missingInSource.push(h);
                });

                if (missingInSource.length > 0) {
                    isAllMatch = false;
                    diffs.push({
                        sourceName: source.name,
                        sourceCount: source.hosts.size,
                        missingCount: missingInSource.length,
                        missingList: missingInSource
                    });
                }
            });

            // 특수 분석: AWX 1000대 중 작업 대상(상세/인시던트)에 없는 호스트 별도 분석
            let awxSpecialReport = '';
            if (state.collectedHosts.size > 0) {
                const targetRef = new Set([...detailList, ...incidentList]);
                const notInTargetFromAwx = [];
                state.collectedHosts.forEach(h => {
                    if (!targetRef.has(h)) notInTargetFromAwx.push(h);
                });

                if (notInTargetFromAwx.length > 0) {
                    isAllMatch = false;
                    const toggleId = 'awx-toggle-' + Date.now();
                    awxSpecialReport = `
                        <div style="margin-top:8px; padding:8px; background:#fff1f2; border:1px solid #fecdd3; border-radius:3px;">
                            <div style="display:flex; justify-content:space-between; align-items:center;">
                                <span style="font-weight:700; color:#be123c;">대상에 없음: ${notInTargetFromAwx.length}대 (AWX ${state.collectedHosts.size}대 중 ${targetRef.size}대만 존재)</span>
                                <button id="btn-${toggleId}" style="background:#ffffff; border:1px solid #f43f5e; color:#be123c; font-size:10px; padding:2px 6px; border-radius:3px; cursor:pointer;">명단 펼치기</button>
                            </div>
                            <div id="${toggleId}" style="display:none; margin-top:6px; max-height:110px; overflow-y:auto; font-family:monospace; font-size:10px; color:#881337; background:#ffffff; padding:6px; border:1px solid #fda4af; word-break:break-all;">
                                ${notInTargetFromAwx.join(' ')}
                            </div>
                        </div>
                    `;
                }
            }

            resultBox.style.display = 'block';

            const mandatoryCommentHtml = `
                <div style="margin-top:10px; padding:8px 10px; background:#f1f5f9; border:1px solid #cbd5e1; border-radius:3px; font-family:Consolas, monospace; font-size:11px; color:#0f172a; word-break:break-all;">
                    인시던트의 작업대상을 복사해서 붙여넣기를 권장합니다.vim [작업대상 파일] -&gt; esc -&gt; :set paste 입력(붙여넣기 모드) -&gt; 붙여넣기
                </div>
            `;

            if (isAllMatch) {
                resultBox.style.background = '#f0fdf4';
                resultBox.style.borderColor = '#86efac';
                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#15803d; font-size:13px; margin-bottom:6px;">
                        [MATCH] 비교 대상 전수 일치 확인 완료
                    </div>
                    <div style="color:#166534; font-size:11px;">
                        • 참여 소스: <b>${activeSources.map(s => `${s.name}(${s.hosts.size}대)`).join(', ')}</b><br>
                        • 불일치 및 누락 호스트: <b>0대</b>
                        ${state.excludedHosts.length > 0 ? `<br>• AWX 제외: <b>${state.rawCount}대 중 ${state.excludedHosts.length}대 제외됨</b> (${state.excludedHosts.join(' ')})` : ''}
                    </div>
                    ${mandatoryCommentHtml}
                `;
            } else {
                resultBox.style.background = '#fef2f2';
                resultBox.style.borderColor = '#fca5a5';
                let diffHtml = '';
                diffs.forEach((d, idx) => {
                    const tid = `diff-toggle-${idx}-${Date.now()}`;
                    diffHtml += `
                        <div style="margin-top:6px; padding:6px; background:#ffffff; border:1px solid #fecaca; border-radius:3px;">
                            <div style="display:flex; justify-content:space-between; align-items:center;">
                                <span style="font-weight:600; color:#991b1b;">• ${d.sourceName}: 누락 ${d.missingCount}대 (보유: ${d.sourceCount}대)</span>
                                <button id="btn-${tid}" style="background:#f8fafc; border:1px solid #cbd5e1; font-size:10px; padding:1px 5px; border-radius:3px; cursor:pointer;">명단 펼치기</button>
                            </div>
                            <div id="${tid}" style="display:none; margin-top:4px; max-height:90px; overflow-y:auto; font-family:monospace; font-size:10px; color:#b91c1c; word-break:break-all;">
                                ${d.missingList.join(' ')}
                            </div>
                        </div>
                    `;
                });

                resultBox.innerHTML = `
                    <div style="font-weight:700; color:#dc2626; font-size:13px; margin-bottom:4px;">
                        [MISMATCH] 소스 간 불일치 감지 (${activeSources.length}개 소스 비교)
                    </div>
                    <div style="color:#7f1d1d; font-size:11px; margin-bottom:6px;">
                        참여 소스 현황: ${activeSources.map(s => `<b>${s.name}</b>(${s.hosts.size}대)`).join(' vs ')}
                    </div>
                    ${awxSpecialReport}
                    ${diffHtml}
                    ${mandatoryCommentHtml}
                `;

                // 토글 버튼 리스너 바인딩
                rootDoc.querySelectorAll('button[id^="btn-"]').forEach(btn => {
                    btn.onclick = () => {
                        const targetId = btn.id.replace('btn-', '');
                        const targetEl = rootDoc.getElementById(targetId);
                        if (targetEl) {
                            const isHidden = targetEl.style.display === 'none';
                            targetEl.style.display = isHidden ? 'block' : 'none';
                            btn.innerText = isHidden ? '명단 접기' : '명단 펼치기';
                        }
                    };
                });
            }
        }

        btnCompare.onclick = doCompare;
        window.awxDoCompare = doCompare;

        updateCountsAndButton();
        renderAwxSummary();
    }

    bindEvents(document, false, null);
})();
