const fs = require('fs');
const v = require('./js/state/validator.js');
require('./sample_data/sample_data.js');

const embedded = global.DCEmbeddedData;

function parseTsvSimple(text) {
  const lines = text.trim().split(/\r?\n/);
  const delim = lines[0].includes('\t') ? '\t' : ',';
  const headers = lines[0].split(delim).map(h => h.trim());
  return lines.slice(1).map(line => {
    const parts = line.split(delim);
    const obj = {};
    headers.forEach((h, i) => obj[h] = parts[i] !== undefined ? parts[i].trim() : '');
    return obj;
  });
}

console.log('=== [테스트 1] 사용자 제공 212행 TSV (X029Y001~003) 52U/48U 서버 영역 검증 ===');
const userRows = parseTsvSimple(embedded.userRawTsv);
const userRes = v.validateCsvAllocations({
  rows: userRows,
  siteLayout: embedded.defaultLayout.sites[0],
  existingDevices: [],
  autoCreateMissingRacks: true
});

console.log('총 입력 행수:', userRes.summary.total);
console.log('정상 할당 (X029Y001 33대 + X029Y002 48대 + X029Y003 34대 = 115대):', userRes.summary.successCount);
console.log('할당 불가 (X029Y002 49번째~145번째 = 97대 48U 서버 범위 초과):', userRes.summary.failCount);
if (userRes.invalidList.length > 0) {
  console.log('첫 번째 초과 항목 샘플:', userRes.invalidList[0].hostname, '->', userRes.invalidList[0].reason);
}

console.log('\n=== [테스트 2] 호스트네임 접두사(^p,^d,^c,^h,^m,^l) 및 공란(임시할당) 판별 검증 ===');
const testCases = [
  ['psvr01', 'HP'],
  ['dsvr01', 'DELL'],
  ['csw01', 'CISCO'],
  ['hsvr01', 'CUSTOM'],
  ['msvr0001', 'SUPERMICRO'],
  ['lsvr01', 'LENOVO'],
  ['', 'TEMP']
];
testCases.forEach(([host, expected]) => {
  const actual = v.determineVendor(host);
  console.log(`hostname='${host || '(공란)'}' -> 판별: ${actual} (기대값: ${expected}) [${actual === expected ? 'PASS' : 'FAIL'}]`);
});

console.log('\n=== [테스트 3] 시뮬레이션 CSV (수동지정U / 자동할당U / 49U 서버할당 불가 / 스위치 50U 허용) ===');
const simCsv = fs.readFileSync('./sample_data/sample_Simulation_New.csv', 'utf-8');
const simRows = parseTsvSimple(simCsv);
const simRes = v.validateCsvAllocations({
  rows: simRows,
  siteLayout: embedded.defaultLayout.sites[0],
  existingDevices: embedded.defaultDevices,
  autoCreateMissingRacks: true,
  isSimulation: true
});

console.log('시뮬레이션 총 행수:', simRes.summary.total);
console.log('정상 할당:', simRes.summary.successCount);
console.log('할당 불가 (오류 검출):', simRes.summary.failCount);
simRes.invalidList.forEach(item => {
  console.log(` - [행 #${item.rowNum}] ${item.rackId} '${item.hostname}': ${item.reason}`);
});
