/**
 * sample_data.js - 다중 X(행)/Y(열) 52U(서버 48U + 네트워크 4U) 랙 레이아웃 + 다양한 벤더(^p,^d,^c,^h,^m,^l,임시할당) + 사용자 제공 212행 CSV 통합 데이터
 */
(function (global) {
  'use strict';

  // 사용자가 제공한 원본 212행 TSV 데이터 (X029Y001: 33대, X029Y002: 145대 -> 48대 정상 / 97대 48U 초과 검출용, X029Y003: 34대)
  const userRawTsv = [
    "모델코드\t랙라벨\thostname\t몇U",
    ...Array.from({ length: 12 }, (_, i) => `012_001\tX029Y001\tmsvr${String(i + 1).padStart(4, '0')}\t1`),
    ...Array.from({ length: 21 }, (_, i) => `012_002\tX029Y001\tmsvr${String(i + 13).padStart(4, '0')}\t1`),
    ...Array.from({ length: 123 }, (_, i) => `012_001\tX029Y002\tmsvr${String(i + 34).padStart(4, '0')}\t1`),
    ...Array.from({ length: 21 }, (_, i) => `012_002\tX029Y002\tmsvr${String(i + 157).padStart(4, '0')}\t1`),
    "031_123\tX029Y002\tmsvr0178\t1",
    ...Array.from({ length: 34 }, (_, i) => `012_001\tX029Y003\tmsvr${String(i + 179).padStart(4, '0')}\t1`)
  ].join('\n');

  // 기본 랙 레이아웃 생성:
  // X001(1행), X002(2행), X003(3행) 각 Y001~Y008 (24개 랙) + X029(4행: Y001~Y008, 8개 랙) = 총 32개 랙!
  // 모든 랙은 기본 maxU: 52, serverMaxU: 48, 통로 시점 점검을 위해 행(X) 간격 4칸으로 배치
  const racks = [];
  const xRows = [1, 2, 3, 29];
  xRows.forEach((xNum, rIdx) => {
    for (let yNum = 1; yNum <= 8; yNum++) {
      const rackId = `X${String(xNum).padStart(3, '0')}Y${String(yNum).padStart(3, '0')}`;
      racks.push({
        rackId,
        name: rackId,
        rackX: xNum,
        rackY: yNum,
        gridX: 3 + (yNum - 1) * 1,
        gridZ: 3 + rIdx * 4,
        widthTiles: 1,
        depthTiles: 2,
        maxU: 52,
        serverMaxU: 48,
        rotationDeg: 0
      });
    }
  });

  const defaultLayout = {
    version: "2.0.0",
    activeSiteId: "SITE-A",
    sites: [
      {
        siteId: "SITE-A",
        name: "제1데이터센터 (SITE A)",
        gridWidth: 26,
        gridDepth: 26,
        tileSizeMm: 600,
        aisleTiles: [],
        racks: racks
      }
    ]
  };

  // 기본 장착 장비 목록 생성
  // 1) X001~X003 랙들: 캡처 4처럼 벤더별(^p:HP, ^d:DELL, ^c:Cisco, ^h:조립서버, ^m:Supermicro, ^l:Lenovo) 및 호스트 미지정(임시할당)이 골고루 분포된 테스트 데이터
  const defaultDevices = [
    // X001Y001 (캡처 4 예시처럼 다중 벤더 + 임시할당 비중 테스트)
    { rackId: 'X001Y001', startU: 1, formFactorU: 2, hostname: 'psvr-db-01', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X001Y001', startU: 3, formFactorU: 2, hostname: 'psvr-db-02', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X001Y001', startU: 5, formFactorU: 4, hostname: 'psvr-hana-01', model: 'HPE-DL580', orientation: 'front' },
    { rackId: 'X001Y001', startU: 9, formFactorU: 2, hostname: 'dsvr-web-01', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X001Y001', startU: 11, formFactorU: 2, hostname: 'dsvr-web-02', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X001Y001', startU: 13, formFactorU: 4, hostname: 'dsvr-ai-01', model: 'DELL-XE8545', orientation: 'front' },
    { rackId: 'X001Y001', startU: 17, formFactorU: 2, hostname: 'msvr-app-01', model: '012_001', orientation: 'front' },
    { rackId: 'X001Y001', startU: 19, formFactorU: 2, hostname: 'msvr-app-02', model: '012_002', orientation: 'front' },
    { rackId: 'X001Y001', startU: 21, formFactorU: 4, hostname: 'lsvr-storage-01', model: 'SR650', orientation: 'front' },
    { rackId: 'X001Y001', startU: 25, formFactorU: 4, hostname: 'hsvr-custom-01', model: 'WHITEBOX-4U', orientation: 'front' },
    { rackId: 'X001Y001', startU: 29, formFactorU: 4, hostname: '', model: '012_002', orientation: 'front', isTemp: true },
    { rackId: 'X001Y001', startU: 33, formFactorU: 4, hostname: '', model: '012_001', orientation: 'front', isTemp: true },
    { rackId: 'X001Y001', startU: 49, formFactorU: 2, hostname: 'csw-tor-01', model: 'Nexus-9300-Switch', orientation: 'back' },

    // X001Y002 (DELL & HP 중심)
    { rackId: 'X001Y002', startU: 1, formFactorU: 2, hostname: 'dsvr-api-01', model: 'DELL-R650', orientation: 'front' },
    { rackId: 'X001Y002', startU: 3, formFactorU: 2, hostname: 'dsvr-api-02', model: 'DELL-R650', orientation: 'front' },
    { rackId: 'X001Y002', startU: 5, formFactorU: 2, hostname: 'dsvr-api-03', model: 'DELL-R650', orientation: 'front' },
    { rackId: 'X001Y002', startU: 7, formFactorU: 4, hostname: 'dsvr-gpu-01', model: 'DELL-R750xa', orientation: 'front' },
    { rackId: 'X001Y002', startU: 11, formFactorU: 4, hostname: 'psvr-core-01', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X001Y002', startU: 15, formFactorU: 4, hostname: 'psvr-core-02', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X001Y002', startU: 19, formFactorU: 2, hostname: '', model: 'DELL-R650', orientation: 'front', isTemp: true },
    { rackId: 'X001Y002', startU: 49, formFactorU: 1, hostname: 'csw-tor-02', model: 'Cisco-Switch', orientation: 'back' },

    // X001Y003 (Lenovo & 조립서버 중심)
    { rackId: 'X001Y003', startU: 1, formFactorU: 4, hostname: 'lsvr-ceph-01', model: 'Lenovo-SR650', orientation: 'front' },
    { rackId: 'X001Y003', startU: 5, formFactorU: 4, hostname: 'lsvr-ceph-02', model: 'Lenovo-SR650', orientation: 'front' },
    { rackId: 'X001Y003', startU: 9, formFactorU: 4, hostname: 'hsvr-node-01', model: 'Custom-2U', orientation: 'front' },
    { rackId: 'X001Y003', startU: 13, formFactorU: 4, hostname: 'hsvr-node-02', model: 'Custom-2U', orientation: 'front' },
    { rackId: 'X001Y003', startU: 17, formFactorU: 4, hostname: '', model: 'Custom-4U', orientation: 'front', isTemp: true },
    { rackId: 'X001Y003', startU: 49, formFactorU: 1, hostname: 'csw-tor-03', model: 'Cisco-Switch', orientation: 'back' },

    // X001Y004 ~ X001Y008
    { rackId: 'X001Y004', startU: 1, formFactorU: 4, hostname: 'psvr-erp-01', model: 'HPE-DL580', orientation: 'front' },
    { rackId: 'X001Y004', startU: 5, formFactorU: 4, hostname: 'psvr-erp-02', model: 'HPE-DL580', orientation: 'front' },
    { rackId: 'X001Y004', startU: 9, formFactorU: 2, hostname: 'msvr-cache-01', model: '012_001', orientation: 'front' },
    { rackId: 'X001Y005', startU: 1, formFactorU: 2, hostname: 'dsvr-k8s-01', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X001Y005', startU: 3, formFactorU: 2, hostname: 'dsvr-k8s-02', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X001Y005', startU: 5, formFactorU: 2, hostname: 'lsvr-k8s-03', model: 'Lenovo-SR630', orientation: 'front' },
    { rackId: 'X001Y005', startU: 7, formFactorU: 4, hostname: '', model: '012_002', orientation: 'front', isTemp: true },
    { rackId: 'X001Y006', startU: 1, formFactorU: 4, hostname: 'hsvr-build-01', model: 'Custom-GPU', orientation: 'front' },
    { rackId: 'X001Y006', startU: 5, formFactorU: 4, hostname: 'msvr-build-02', model: '012_001', orientation: 'front' },
    { rackId: 'X001Y007', startU: 1, formFactorU: 4, hostname: 'dsvr-elastic-01', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X001Y007', startU: 5, formFactorU: 4, hostname: 'psvr-elastic-02', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X001Y008', startU: 1, formFactorU: 2, hostname: 'csw-core-01', model: 'Cisco-Nexus', orientation: 'front' },
    { rackId: 'X001Y008', startU: 3, formFactorU: 2, hostname: 'csw-core-02', model: 'Cisco-Nexus', orientation: 'front' },

    // 2행 (X002Y001 ~ X002Y008)
    { rackId: 'X002Y001', startU: 1, formFactorU: 4, hostname: 'dsvr-batch-01', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X002Y001', startU: 5, formFactorU: 4, hostname: 'lsvr-batch-02', model: 'Lenovo-SR650', orientation: 'front' },
    { rackId: 'X002Y001', startU: 9, formFactorU: 4, hostname: '', model: '012_001', orientation: 'front', isTemp: true },
    { rackId: 'X002Y002', startU: 1, formFactorU: 4, hostname: 'psvr-dw-01', model: 'HPE-DL380', orientation: 'front' },
    { rackId: 'X002Y002', startU: 5, formFactorU: 4, hostname: 'msvr-dw-02', model: '012_002', orientation: 'front' },
    { rackId: 'X002Y003', startU: 1, formFactorU: 4, hostname: 'hsvr-ai-01', model: 'Custom-AI', orientation: 'front' },
    { rackId: 'X002Y003', startU: 5, formFactorU: 4, hostname: 'dsvr-ai-02', model: 'DELL-XE8545', orientation: 'front' },
    { rackId: 'X002Y004', startU: 1, formFactorU: 2, hostname: 'lsvr-was-01', model: 'Lenovo-SR630', orientation: 'front' },
    { rackId: 'X002Y004', startU: 3, formFactorU: 2, hostname: 'psvr-was-02', model: 'HPE-DL360', orientation: 'front' },
    { rackId: 'X002Y005', startU: 1, formFactorU: 4, hostname: '', model: '012_001', orientation: 'front', isTemp: true },
    { rackId: 'X002Y006', startU: 1, formFactorU: 4, hostname: 'msvr-hadoop-01', model: '012_002', orientation: 'front' },
    { rackId: 'X002Y007', startU: 1, formFactorU: 4, hostname: 'dsvr-hadoop-02', model: 'DELL-R750', orientation: 'front' },
    { rackId: 'X002Y008', startU: 1, formFactorU: 4, hostname: 'psvr-backup-01', model: 'HPE-Apollo', orientation: 'front' },

    // 3행 (X003Y001 ~ X003Y008)
    { rackId: 'X003Y001', startU: 1, formFactorU: 4, hostname: 'lsvr-vdi-01', model: 'Lenovo-SR650', orientation: 'front' },
    { rackId: 'X003Y001', startU: 5, formFactorU: 4, hostname: 'hsvr-vdi-02', model: 'Custom-VDI', orientation: 'front' },
    { rackId: 'X003Y002', startU: 1, formFactorU: 4, hostname: 'dsvr-sec-01', model: 'DELL-R650', orientation: 'front' },
    { rackId: 'X003Y003', startU: 1, formFactorU: 4, hostname: 'psvr-sec-02', model: 'HPE-DL360', orientation: 'front' },
    { rackId: 'X003Y004', startU: 1, formFactorU: 4, hostname: '', model: '012_002', orientation: 'front', isTemp: true }
  ];

  // 2) 사용자가 직접 붙여넣어 주신 X029Y001(33대), X029Y002(1~48U 정상 할당분 48대), X029Y003(34대) 자동 적재
  // X029Y001: msvr0001~0012 (012_001, 1U), msvr0013~0033 (012_002, 1U) -> 1U~33U
  for (let i = 1; i <= 33; i++) {
    defaultDevices.push({
      rackId: 'X029Y001',
      startU: i,
      formFactorU: 1,
      hostname: `msvr${String(i).padStart(4, '0')}`,
      model: i <= 12 ? '012_001' : '012_002',
      orientation: 'front'
    });
  }

  // X029Y002: msvr0034~msvr0081 (48U 서버 영역 꽉 채움, 49번째인 msvr0082~msvr0178은 [🧪 샘플 CSV 검증 테스트] 클릭 시 48U 초과 리포트로 확인 가능)
  for (let i = 1; i <= 48; i++) {
    const hostNum = 33 + i;
    defaultDevices.push({
      rackId: 'X029Y002',
      startU: i,
      formFactorU: 1,
      hostname: `msvr${String(hostNum).padStart(4, '0')}`,
      model: '012_001',
      orientation: 'front'
    });
  }

  // X029Y003: msvr0179~msvr0212 (34대, 1U~34U)
  for (let i = 1; i <= 34; i++) {
    const hostNum = 178 + i;
    defaultDevices.push({
      rackId: 'X029Y003',
      startU: i,
      formFactorU: 1,
      hostname: `msvr${String(hostNum).padStart(4, '0')}`,
      model: '012_001',
      orientation: 'front'
    });
  }

  const DCEmbeddedData = {
    defaultLayout,
    defaultDevices,
    userRawTsv
  };

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = DCEmbeddedData;
  }
  global.DCEmbeddedData = DCEmbeddedData;

})(typeof window !== 'undefined' ? window : globalThis);

