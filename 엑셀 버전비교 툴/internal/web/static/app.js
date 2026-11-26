"use strict";
// 파일명·시트명 같은 파일에서 온 문자열은 textContent 로만 화면에 넣는다.

const token = location.hash.slice(1);
const $ = (id) => document.getElementById(id);
const sides = ["old", "new"];
const uploading = {}; // side -> 전송 중인 XMLHttpRequest
const uploadError = {};

let state = {};
let tableKey = ""; // 지금 표를 만든 파일 id. 바뀔 때만 다시 만들어 선택을 유지한다
let rows = []; // 새 시트·삭제 시트 한 줄씩: {checked, old, new, oldKey, newKey}
let failures = 0;
let timer = 0;
let starting = false;

async function api(path, options = {}) {
  const res = await fetch(path, { ...options, headers: { "X-Token": token } });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || "요청에 실패했습니다 (" + res.status + ")");
  return body;
}

// 상태 조회는 하트비트를 겸한다.
async function poll() {
  clearTimeout(timer);
  try {
    state = await api("/api/state");
    failures = 0;
    render();
  } catch (e) {
    failures++;
    if (failures === 1) $("message").textContent = e.message;
  }
  $("offline").hidden = failures < 3;
  const busy = state.job?.status === "running" || sides.some((s) => uploading[s] || ["uploading", "reading"].includes(state[s]?.status));
  clearTimeout(timer);
  timer = setTimeout(poll, busy ? 500 : 3000);
}

function upload(side, file) {
  uploading[side]?.abort();
  delete uploadError[side];
  $("message").textContent = "";
  const xhr = new XMLHttpRequest();
  uploading[side] = xhr;
  xhr.fileName = file.name;
  xhr.percent = 0;
  xhr.open("POST", "/api/upload?" + new URLSearchParams({ side, name: file.name }));
  xhr.setRequestHeader("X-Token", token);
  xhr.upload.onprogress = (e) => {
    if (e.lengthComputable) xhr.percent = Math.floor((e.loaded * 100) / e.total);
    render();
  };
  xhr.onload = () => {
    if (xhr.status !== 200) {
      let message = "업로드에 실패했습니다 (" + xhr.status + ")";
      try { message = JSON.parse(xhr.responseText).error || message; } catch { /* JSON 이 아닌 응답 */ }
      uploadError[side] = message;
    }
  };
  xhr.onerror = () => { uploadError[side] = "업로드에 실패했습니다. 파일을 읽을 수 있는지 확인하고 다시 선택하세요."; };
  xhr.onloadend = async () => {
    if (uploading[side] !== xhr) return; // 다른 파일로 바꿨다
    try { state = await api("/api/state"); } catch { /* 다음 조회에서 처리 */ }
    if (uploading[side] !== xhr) return;
    delete uploading[side];
    render();
    poll();
  };
  xhr.send(file);
  render();
  poll();
}

function renderFile(side) {
  const card = $(side);
  const bar = card.querySelector("progress");
  const status = card.querySelector(".status");
  const f = state[side];
  let text = "파일을 선택하세요 (.xlsx, .xlsm)";
  let percent = null;
  let bad = false;
  if (uploading[side]) {
    percent = uploading[side].percent;
    text = uploading[side].fileName + " — 올리는 중 " + percent + "%";
  } else if (uploadError[side]) {
    text = uploadError[side];
    bad = true;
  } else if (f) {
    if (f.status === "ready") {
      text = f.name + " — 시트 " + (f.sheets?.length || 0) + "개";
    } else if (f.status === "error") {
      text = f.error;
      bad = true;
    } else if (f.status === "reading") {
      percent = f.percent;
      text = f.name + " — 읽는 중 " + percent + "%";
    } else {
      text = f.name + " — 올리는 중";
    }
  }
  status.textContent = text;
  status.classList.toggle("error", bad);
  bar.hidden = percent === null;
  if (percent !== null) bar.value = percent;
  card.querySelector("input").disabled = state.job?.status === "running";
}

const ready = () => sides.every((s) => !uploading[s] && !uploadError[s] && state[s]?.status === "ready");

function select(options, value, onChange) {
  const el = document.createElement("select");
  for (const [v, label] of options) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = label;
    el.append(o);
  }
  el.value = value;
  el.addEventListener("change", () => onChange(el.value));
  return el;
}

const sheetOf = (side, name) => state[side].sheets?.find((s) => s.name === name);
const sheetLabel = (s) => s.name + (s.hidden ? " (숨김)" : "");

// 최신 시트에 짝지은 이전 시트를 뺀 나머지는 '삭제된 시트' 줄이 된다.
function syncDeleted() {
  const used = new Set(rows.filter((r) => r.new).map((r) => r.old));
  rows = rows.filter((r) => r.new || !used.has(r.old));
  for (const s of state.old.sheets || []) {
    if (!used.has(s.name) && !rows.some((r) => !r.new && r.old === s.name)) {
      rows.push({ checked: true, old: s.name, new: "", oldKey: 0, newKey: 0 });
    }
  }
}

function keySelect(row, side) {
  const key = side === "old" ? "oldKey" : "newKey";
  const sheet = sheetOf(side, row[side]);
  const options = [[0, "자동"]].concat((sheet?.columns || []).map((c) => [c.num, c.letter + "열" + (c.header ? " · " + c.header : "")]));
  const el = select(options, row[key], (v) => {
    row[key] = Number(v);
    // 한쪽을 고르면 다른 쪽은 같은 머리글(없으면 같은 열)로 맞춘다. 자동은 양쪽 모두 자동이다.
    const other = side === "old" ? "new" : "old";
    const otherKey = side === "old" ? "newKey" : "oldKey";
    if (row[key] === 0) {
      row[otherKey] = 0;
    } else if (row[otherKey] === 0) {
      const header = sheet.columns.find((c) => c.num === row[key])?.header;
      const cols = sheetOf(other, row[other])?.columns || [];
      const match = cols.find((c) => header && c.header === header) || cols.find((c) => c.num === row[key]);
      row[otherKey] = match ? match.num : 0;
    }
    renderTable();
  });
  el.disabled = !row.checked || !row.old || !row.new;
  el.className = key;
  el.setAttribute("aria-label", side === "old" ? "이전 키 열" : "최신 키 열");
  return el;
}

function renderTable() {
  const body = $("pairs");
  body.replaceChildren();
  for (const row of rows) {
    const tr = document.createElement("tr");
    tr.classList.toggle("off", !row.checked);
    const cell = (content) => {
      const td = document.createElement("td");
      td.append(content);
      tr.append(td);
    };
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = row.checked;
    box.setAttribute("aria-label", "비교에 포함");
    box.addEventListener("change", () => {
      row.checked = box.checked;
      renderTable();
    });
    cell(box);
    if (row.new) {
      const options = [["", "(없음: 추가된 시트)"]].concat((state.old.sheets || []).map((s) => [s.name, sheetLabel(s)]));
      const el = select(options, row.old, (v) => {
        for (const r of rows) {
          if (v && r.new && r !== row && r.old === v) Object.assign(r, { old: "", oldKey: 0, newKey: 0 });
        }
        Object.assign(row, { old: v, oldKey: 0, newKey: 0 });
        syncDeleted();
        renderTable();
      });
      el.className = "oldSheet";
      el.setAttribute("aria-label", "이전 시트");
      cell(el);
      cell(sheetLabel(sheetOf("new", row.new)));
    } else {
      cell(sheetLabel(sheetOf("old", row.old)));
      cell("(없음: 삭제된 시트)");
    }
    cell(keySelect(row, "old"));
    cell(keySelect(row, "new"));
    body.append(tr);
  }
  const on = rows.filter((r) => r.checked).length;
  $("all").checked = on === rows.length;
  $("all").indeterminate = on > 0 && on < rows.length;
  updateButton();
}

function updateButton() {
  $("compare").disabled = starting || !ready() || state.job?.status === "running" || !rows.some((r) => r.checked);
  for (const el of $("sheets").querySelectorAll("input, select")) {
    if (state.job?.status === "running") el.disabled = true;
  }
}

const number = (n) => n.toLocaleString("ko-KR");

function duration(ms) {
  const s = Math.floor(ms / 1000);
  return s < 60 ? s + "초" : Math.floor(s / 60) + "분 " + (s % 60) + "초";
}

function renderJob() {
  const job = state.job;
  $("job").hidden = !job;
  if (!job) return;
  $("jobTitle").textContent = { running: "비교하는 중", done: "비교 완료", error: "비교 실패" }[job.status];
  $("running").hidden = job.status !== "running";
  $("bar").value = job.percent;
  $("stage").textContent = job.stage;
  $("percent").textContent = job.percent + "%";
  $("elapsed").textContent = duration(job.elapsedMs);
  $("jobError").textContent = job.error || "";
  $("result").hidden = !job.result;
  const r = job.result;
  if (!r || $("result").dataset.path === r.path) return;
  $("result").dataset.path = r.path;
  $("openMessage").textContent = "";
  const s = r.summary;
  const lines = [
    ["이전 파일", r.oldName],
    ["최신 파일", r.newName],
    ["변경내역", r.items ? number(r.items) + "건" : "변경 사항이 없습니다"],
    ["시트", "추가 " + s.SheetsAdded + " · 삭제 " + s.SheetsDeleted + " · 이름 다른 짝 " + s.SheetsRenamed],
    ["행", "추가 " + number(s.RowsAdded) + " · 삭제 " + number(s.RowsDeleted) + " · 수정 " + number(s.RowsModified) + " · 이동 " + number(s.RowsMoved) + " (그중 값 변경 " + number(s.RowsMovedModified) + ")"],
    ["열", "추가 " + number(s.ColsAdded) + " · 삭제 " + number(s.ColsDeleted) + " · 이동 " + number(s.ColsMoved)],
    ["셀", "수정 " + number(s.ModifiedCells) + " · 추가 " + number(s.AddedCells) + " · 삭제 " + number(s.DeletedCells) + " · 확인 불가 " + number(s.UnknownCells)],
    ["소요 시간", duration(job.elapsedMs)],
    ["결과 파일", r.path],
  ];
  const body = $("summary").tBodies[0];
  body.replaceChildren();
  for (const [name, value] of lines) {
    const tr = body.insertRow();
    const th = document.createElement("th");
    th.textContent = name;
    tr.append(th);
    tr.insertCell().textContent = value;
  }
  $("warningBox").hidden = r.warningCount === 0;
  $("warningTitle").textContent = "경고 " + number(r.warningCount) + "건" +
    (r.warningCount > r.warnings.length ? " (처음 " + r.warnings.length + "건 표시, 전체는 결과 파일의 요약 시트)" : "");
  $("warnings").replaceChildren(...r.warnings.map((w) => {
    const li = document.createElement("li");
    li.textContent = w;
    return li;
  }));
}

function render() {
  sides.forEach(renderFile);
  $("sheets").hidden = !ready();
  if (ready()) {
    const key = state.old.id + "/" + state.new.id;
    if (key !== tableKey) {
      tableKey = key;
      rows = (state.pairs || []).map((p) => ({ checked: true, old: p.old, new: p.new, oldKey: 0, newKey: 0 }));
      renderTable();
      $("message").textContent = "";
    }
  }
  const running = state.job?.status === "running";
  if ($("sheets").dataset.running !== String(running)) {
    $("sheets").dataset.running = String(running);
    if (ready()) {
      $("all").disabled = false;
      renderTable();
    }
  }
  updateButton();
  renderJob();
}

async function compare() {
  $("message").textContent = "";
  const chosen = rows.filter((r) => r.checked);
  const half = chosen.find((r) => r.old && r.new && !r.oldKey !== !r.newKey);
  if (half) {
    $("message").textContent = "시트 '" + half.new + "': 키 열은 이전과 최신을 모두 지정하거나 모두 '자동'으로 두세요.";
    return;
  }
  starting = true;
  updateButton();
  try {
    await api("/api/compare", {
      method: "POST",
      body: JSON.stringify({ pairs: chosen.map(({ old, new: n, oldKey, newKey }) => ({ old, new: n, oldKey, newKey })) }),
    });
    delete $("result").dataset.path;
  } catch (e) {
    $("message").textContent = e.message;
  }
  try { state = await api("/api/state"); } catch { /* 다음 조회에서 처리 */ }
  starting = false;
  render();
  poll();
}

async function openResult(what) {
  $("openMessage").textContent = "";
  $("openMessage").className = "";
  try {
    await api("/api/open?what=" + what, { method: "POST" });
    $("openMessage").textContent = what === "file" ? "결과 파일을 여는 중입니다." : "폴더를 열었습니다.";
  } catch (e) {
    $("openMessage").textContent = e.message;
    $("openMessage").className = "error";
  }
}

for (const side of sides) {
  const input = $(side).querySelector("input");
  input.addEventListener("click", () => { input.value = ""; }); // 같은 파일을 다시 골라도 올린다
  input.addEventListener("change", () => { if (input.files[0]) upload(side, input.files[0]); });
}
$("all").addEventListener("change", () => {
  for (const r of rows) r.checked = $("all").checked;
  renderTable();
});
$("compare").addEventListener("click", compare);
$("openFile").addEventListener("click", () => openResult("file"));
$("openFolder").addEventListener("click", () => openResult("folder"));
// 파일 칸 밖에 파일을 끌어다 놓으면 화면이 그 파일로 바뀌므로 막는다.
for (const type of ["dragover", "drop"]) {
  window.addEventListener(type, (e) => { if (e.target.type !== "file") e.preventDefault(); });
}
window.addEventListener("pagehide", () => navigator.sendBeacon("/api/closing?t=" + token));
poll();
