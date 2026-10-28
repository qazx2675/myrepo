// SOP Generator Frontend Application Logic
document.addEventListener("DOMContentLoaded", () => {
  // State
  let appConfig = null;
  let targetSheets = [];
  let selectedQueue = []; // array of sheet names in check order: [sheetName1, sheetName2, ...]
  let lastGeneratedPath = "";

  // DOM Elements
  const currentWorkerName = document.getElementById("currentWorkerName");
  const btnChangeWorker = document.getElementById("btnChangeWorker");
  const btnRefreshAll = document.getElementById("btnRefreshAll");
  const btnExitApp = document.getElementById("btnExitApp");

  // Worker Modal Elements
  const modalWorker = document.getElementById("modalWorker");
  const modalWorkerTitle = document.getElementById("modalWorkerTitle");
  const modalWorkerDesc = document.getElementById("modalWorkerDesc");
  const inputModalWorkerName = document.getElementById("inputModalWorkerName");
  const modalWorkerError = document.getElementById("modalWorkerError");
  const btnCancelWorkerModal = document.getElementById("btnCancelWorkerModal");
  const btnSaveWorkerName = document.getElementById("btnSaveWorkerName");

  // Target Elements
  const inputTargetFile = document.getElementById("inputTargetFile");
  const btnScanSheets = document.getElementById("btnScanSheets");
  const btnCreateSampleTarget = document.getElementById("btnCreateSampleTarget");
  const btnScanInitial = document.getElementById("btnScanInitial");
  const sheetsLoading = document.getElementById("sheetsLoading");
  const sheetsEmpty = document.getElementById("sheetsEmpty");
  const sheetsList = document.getElementById("sheetsList");
  const btnClearOrder = document.getElementById("btnClearOrder");
  const orderQueueText = document.getElementById("orderQueueText");

  // Template Elements
  const selectTemplate = document.getElementById("selectTemplate");
  const btnCreateDefaultTmpl = document.getElementById("btnCreateDefaultTmpl");
  const fileUploadTemplate = document.getElementById("fileUploadTemplate");

  // Classification & Detail Elements
  const selectMajor = document.getElementById("selectMajor");
  const inputCustomMajor = document.getElementById("inputCustomMajor");
  const selectMinor = document.getElementById("selectMinor");
  const inputCustomMinor = document.getElementById("inputCustomMinor");
  const textareaDetail = document.getElementById("textareaDetail");
  const inputDstFilename = document.getElementById("inputDstFilename");

  // Generation Elements
  const btnGenerateSOP = document.getElementById("btnGenerateSOP");
  const genStatusBox = document.getElementById("genStatusBox");
  const genStatusText = document.getElementById("genStatusText");
  const genResultCard = document.getElementById("genResultCard");
  const resDstFile = document.getElementById("resDstFile");
  const resFileSize = document.getElementById("resFileSize");
  const resExecTime = document.getElementById("resExecTime");
  const btnOpenGeneratedFile = document.getElementById("btnOpenGeneratedFile");
  const btnOpenFolder = document.getElementById("btnOpenFolder");

  // Log Console
  const logConsole = document.getElementById("logConsole");
  const btnClearLog = document.getElementById("btnClearLog");

  function log(message, type = "info") {
    const entry = document.createElement("div");
    entry.className = `log-entry log-${type}`;
    const now = new Date().toLocaleTimeString();
    entry.textContent = `[${now}] ${message}`;
    logConsole.appendChild(entry);
    logConsole.scrollTop = logConsole.scrollHeight;
  }

  // 1. Initial Load
  async function loadConfig() {
    try {
      const res = await fetch("/api/config");
      const data = await res.json();
      if (data.success && data.config) {
        appConfig = data.config;
        renderConfigUI();

        // Check worker name: if empty, show mandatory modal
        if (!appConfig.worker_name || appConfig.worker_name.trim() === "") {
          showWorkerModal(true);
        } else {
          log(`작업자 확인: ${appConfig.worker_name}`, "info");
        }
      }
    } catch (err) {
      log(`설정 불러오기 실패: ${err.message}`, "error");
    }
  }

  function renderConfigUI() {
    if (!appConfig) return;

    // Worker name
    const wName = appConfig.worker_name ? appConfig.worker_name.trim() : "";
    currentWorkerName.textContent = wName || "미설정";

    // Categories (Major)
    selectMajor.innerHTML = '<option value="">-- 대분류 선택 --</option>';
    (appConfig.major_categories || []).forEach(cat => {
      const opt = document.createElement("option");
      opt.value = cat;
      opt.textContent = cat;
      selectMajor.appendChild(opt);
    });
    if (appConfig.major_categories && appConfig.major_categories.length > 0) {
      selectMajor.value = appConfig.major_categories[0];
    }

    // Categories (Minor)
    selectMinor.innerHTML = '<option value="">-- 중분류 선택 --</option>';
    (appConfig.minor_categories || []).forEach(cat => {
      const opt = document.createElement("option");
      opt.value = cat;
      opt.textContent = cat;
      selectMinor.appendChild(opt);
    });
    if (appConfig.minor_categories && appConfig.minor_categories.length > 0) {
      selectMinor.value = appConfig.minor_categories[0];
    }

    // Templates
    selectTemplate.innerHTML = "";
    (appConfig.templates || []).forEach(tmpl => {
      const opt = document.createElement("option");
      opt.value = tmpl.id;
      opt.textContent = tmpl.name;
      selectTemplate.appendChild(opt);
    });
    if (appConfig.selected_template) {
      selectTemplate.value = appConfig.selected_template;
    }

    // Last used inputs
    if (appConfig.last_target_file) {
      inputTargetFile.value = appConfig.last_target_file;
    }
    if (appConfig.last_dst_filename) {
      inputDstFilename.value = appConfig.last_dst_filename;
    }
    if (appConfig.last_detail) {
      textareaDetail.value = appConfig.last_detail;
    }
  }

  // 2. Worker Modal Logic
  function showWorkerModal(isMandatory = false) {
    modalWorker.classList.remove("hidden");
    inputModalWorkerName.value = appConfig?.worker_name || "";
    modalWorkerError.classList.add("hidden");

    if (isMandatory) {
      modalWorkerTitle.textContent = "작업자 등록 (최초 1회 필수)";
      modalWorkerDesc.textContent = "프로그램을 사용하기 위해 작업자 이름을 먼저 입력해주세요.";
      btnCancelWorkerModal.classList.add("hidden");
    } else {
      modalWorkerTitle.textContent = "작업자 이름 변경";
      modalWorkerDesc.textContent = "새로운 작업자 이름을 입력하면 설정에 즉시 영구 저장됩니다.";
      btnCancelWorkerModal.classList.remove("hidden");
    }
    inputModalWorkerName.focus();
  }

  function hideWorkerModal() {
    modalWorker.classList.add("hidden");
  }

  btnChangeWorker.addEventListener("click", () => {
    showWorkerModal(false);
  });

  btnCancelWorkerModal.addEventListener("click", hideWorkerModal);

  btnSaveWorkerName.addEventListener("click", async () => {
    const name = inputModalWorkerName.value.trim();
    if (!name) {
      modalWorkerError.textContent = "작업자 이름을 입력해주세요.";
      modalWorkerError.classList.remove("hidden");
      return;
    }

    try {
      const res = await fetch("/api/config/worker", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ worker_name: name })
      });
      const data = await res.json();
      if (data.success) {
        if (!appConfig) appConfig = {};
        appConfig.worker_name = name;
        currentWorkerName.textContent = name;
        hideWorkerModal();
        log(`작업자 이름 저장 완료: ${name}`, "success");
      } else {
        modalWorkerError.textContent = data.message || "저장 실패";
        modalWorkerError.classList.remove("hidden");
      }
    } catch (err) {
      modalWorkerError.textContent = "서버 통신 오류: " + err.message;
      modalWorkerError.classList.remove("hidden");
    }
  });

  inputModalWorkerName.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      btnSaveWorkerName.click();
    }
  });

  // 3. Scan Target Sheets
  async function scanSheets() {
    const target = inputTargetFile.value.trim() || "target.xlsx";
    sheetsLoading.classList.remove("hidden");
    sheetsEmpty.classList.add("hidden");
    sheetsList.innerHTML = "";

    try {
      log(`Target 엑셀 시트 스캔 중: ${target}...`, "info");
      const res = await fetch(`/api/target/sheets?target=${encodeURIComponent(target)}`);
      const data = await res.json();

      sheetsLoading.classList.add("hidden");

      if (data.success && data.sheets && data.sheets.length > 0) {
        targetSheets = data.sheets;
        renderSheetsList();
        log(`시트 ${data.count}개 스캔 완료.`, "success");
      } else {
        sheetsEmpty.classList.remove("hidden");
        sheetsEmpty.querySelector("p").textContent = data.message || "시트를 찾을 수 없습니다.";
        log(`시트 스캔 실패: ${data.message}`, "error");
      }
    } catch (err) {
      sheetsLoading.classList.add("hidden");
      sheetsEmpty.classList.remove("hidden");
      log(`시트 스캔 통신 오류: ${err.message}`, "error");
    }
  }

  function renderSheetsList() {
    sheetsList.innerHTML = "";
    if (targetSheets.length === 0) {
      sheetsEmpty.classList.remove("hidden");
      return;
    }
    sheetsEmpty.classList.add("hidden");

    targetSheets.forEach(sheet => {
      const item = document.createElement("div");
      item.className = "sheet-item";
      item.dataset.sheetName = sheet.name;

      const orderIdx = selectedQueue.indexOf(sheet.name);
      const isSelected = orderIdx !== -1;
      if (isSelected) {
        item.classList.add("selected");
      }

      item.innerHTML = `
        <div class="sheet-item-left">
          <input type="checkbox" class="sheet-checkbox" ${isSelected ? "checked" : ""}>
          <span class="order-badge ${isSelected ? "" : "empty"}">${isSelected ? (orderIdx + 1) : ""}</span>
          <div class="sheet-info-col">
            <span class="sheet-name">${escapeHtml(sheet.name)}</span>
            <span class="sheet-summary">${escapeHtml(sheet.h2_summary || "(H2 본문 없음)")}</span>
          </div>
        </div>
        <div class="sheet-badges">
          ${sheet.shape_count > 0 ? `<span class="shape-badge">🖼️ 도형/이미지 ${sheet.shape_count}개</span>` : ""}
        </div>
      `;

      // Click event on item row
      item.addEventListener("click", (e) => {
        if (e.target.tagName !== "INPUT") {
          const chk = item.querySelector(".sheet-checkbox");
          chk.checked = !chk.checked;
        }
        toggleSheetSelection(sheet.name);
      });

      sheetsList.appendChild(item);
    });

    updateOrderSummary();
  }

  function toggleSheetSelection(sheetName) {
    const idx = selectedQueue.indexOf(sheetName);
    if (idx === -1) {
      selectedQueue.push(sheetName);
    } else {
      selectedQueue.splice(idx, 1);
    }
    renderSheetsList();
  }

  function updateOrderSummary() {
    if (selectedQueue.length === 0) {
      orderQueueText.textContent = "선택된 작업이 없습니다 (체크박스를 클릭하여 순서를 정하세요)";
      orderQueueText.classList.remove("active");
    } else {
      const summary = selectedQueue.map((s, idx) => `${idx + 1}. [${s}]`).join(" ➔ ");
      orderQueueText.textContent = summary;
      orderQueueText.classList.add("active");
    }
  }

  btnClearOrder.addEventListener("click", () => {
    selectedQueue = [];
    renderSheetsList();
    log("선택된 작업 순서가 초기화되었습니다.", "info");
  });

  btnScanSheets.addEventListener("click", scanSheets);
  btnScanInitial.addEventListener("click", scanSheets);

  // 4. Sample Target Generation
  btnCreateSampleTarget.addEventListener("click", async () => {
    try {
      log("3개 시트가 포함된 테스트용 target.xlsx 생성 중...", "info");
      const res = await fetch("/api/create-sample-target", { method: "POST" });
      const data = await res.json();
      if (data.success) {
        log("샘플 target.xlsx 파일 생성 완료! 시트를 자동으로 스캔합니다.", "success");
        inputTargetFile.value = "target.xlsx";
        await scanSheets();
      } else {
        log(`샘플 생성 실패: ${data.message}`, "error");
      }
    } catch (err) {
      log(`샘플 생성 통신 오류: ${err.message}`, "error");
    }
  });

  // 5. Template Actions
  btnCreateDefaultTmpl.addEventListener("click", async () => {
    try {
      log("기본 표준 양식 파일(default_template.xlsx) 생성 중...", "info");
      const res = await fetch("/api/templates/create-default", { method: "POST" });
      const data = await res.json();
      if (data.success) {
        log(data.message, "success");
        await loadConfig();
      } else {
        log(`양식 생성 실패: ${data.message}`, "error");
      }
    } catch (err) {
      log(`양식 생성 통신 오류: ${err.message}`, "error");
    }
  });

  fileUploadTemplate.addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;

    const formData = new FormData();
    formData.append("template_file", file);

    try {
      log(`양식 파일 업로드 중: ${file.name}...`, "info");
      const res = await fetch("/api/templates/upload", {
        method: "POST",
        body: formData
      });
      const data = await res.json();
      if (data.success) {
        log(data.message, "success");
        await loadConfig();
      } else {
        log(`양식 업로드 실패: ${data.message}`, "error");
      }
    } catch (err) {
      log(`양식 업로드 오류: ${err.message}`, "error");
    }
    fileUploadTemplate.value = "";
  });

  selectTemplate.addEventListener("change", async () => {
    const tmplId = selectTemplate.value;
    try {
      await fetch("/api/templates/select", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ template_id: tmplId })
      });
      log(`양식 선택 변경: ${tmplId}`, "info");
    } catch (err) {
      console.error(err);
    }
  });

  // 6. SOP Generation Action
  btnGenerateSOP.addEventListener("click", async () => {
    if (selectedQueue.length === 0) {
      alert("작업방법에 포함할 시트를 1개 이상 선택해주세요.");
      return;
    }

    // Category values (custom text overrides select if provided)
    const major = inputCustomMajor.value.trim() || selectMajor.value.trim();
    if (!major) {
      alert("대분류를 선택하거나 직접 입력해주세요.");
      inputCustomMajor.focus();
      return;
    }

    const minor = inputCustomMinor.value.trim() || selectMinor.value.trim();
    if (!minor) {
      alert("중분류를 선택하거나 직접 입력해주세요.");
      inputCustomMinor.focus();
      return;
    }

    const targetFile = inputTargetFile.value.trim() || "target.xlsx";
    const dstFilename = inputDstFilename.value.trim() || "SOP_완료.xlsx";
    const detailText = textareaDetail.value;

    const reqPayload = {
      target_path: targetFile,
      major_category: major,
      minor_category: minor,
      detail_text: detailText,
      selected_sheets: selectedQueue,
      dst_filename: dstFilename
    };

    // UI State: Loading
    btnGenerateSOP.disabled = true;
    genStatusBox.classList.remove("hidden");
    genResultCard.classList.add("hidden");
    genStatusText.textContent = `SOP 엑셀 생성 중... (${selectedQueue.length}개 작업 조합)`;

    log(`[SOP 생성 시작] 대분류: ${major}, 중분류: ${minor}, 파일명: ${dstFilename}`, "info");
    log(`작업 순서: ${selectedQueue.map((s, i) => `${i + 1}.${s}`).join(" -> ")}`, "info");

    try {
      const res = await fetch("/api/generate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(reqPayload)
      });
      const data = await res.json();

      btnGenerateSOP.disabled = false;
      genStatusBox.classList.add("hidden");

      if (data.success && data.result) {
        lastGeneratedPath = data.result.dst_path;
        resDstFile.textContent = `${data.result.dst_filename} (${data.result.dst_path})`;
        resFileSize.textContent = formatBytes(data.result.file_size);
        resExecTime.textContent = data.result.execution_time;

        genResultCard.classList.remove("hidden");
        log(`✓ ${data.message} (소요 시간: ${data.result.execution_time})`, "success");
        if (data.config) {
          appConfig = data.config;
          renderConfigUI();
        }
      } else {
        log(`❌ SOP 생성 실패: ${data.message}`, "error");
        alert("SOP 생성 실패: " + data.message);
      }
    } catch (err) {
      btnGenerateSOP.disabled = false;
      genStatusBox.classList.add("hidden");
      log(`❌ 서버 통신 오류: ${err.message}`, "error");
      alert("통신 오류: " + err.message);
    }
  });

  // 7. Open File / Folder
  btnOpenGeneratedFile.addEventListener("click", async () => {
    if (!lastGeneratedPath) return;
    try {
      await fetch("/api/open-file", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: lastGeneratedPath, open_folder: false })
      });
      log(`파일 열기 요청 전송: ${lastGeneratedPath}`, "info");
    } catch (err) {
      console.error(err);
    }
  });

  btnOpenFolder.addEventListener("click", async () => {
    if (!lastGeneratedPath) return;
    try {
      await fetch("/api/open-file", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: lastGeneratedPath, open_folder: true })
      });
      log(`폴더 열기 요청 전송: ${lastGeneratedPath}`, "info");
    } catch (err) {
      console.error(err);
    }
  });

  // 8. General Actions
  btnRefreshAll.addEventListener("click", async () => {
    await loadConfig();
    await scanSheets();
    log("전체 상태 새로고침 완료.", "info");
  });

  btnExitApp.addEventListener("click", async () => {
    if (confirm("프로그램을 종료하시겠습니까?")) {
      try {
        await fetch("/api/exit", { method: "POST" });
        window.close();
      } catch (err) {
        window.close();
      }
    }
  });

  btnClearLog.addEventListener("click", () => {
    logConsole.innerHTML = "";
  });

  // Heartbeat loop (every 5s)
  setInterval(async () => {
    try {
      await fetch("/api/heartbeat");
    } catch (e) {
      // ignore
    }
  }, 5000);

  function escapeHtml(str) {
    if (!str) return "";
    return str.replace(/[&<>"']/g, (m) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;"
    }[m]));
  }

  function formatBytes(bytes) {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
  }

  // Initialize
  (async function init() {
    await loadConfig();
    await scanSheets();
  })();
});
