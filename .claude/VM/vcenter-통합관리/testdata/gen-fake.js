#!/usr/bin/env node
'use strict';
// 가짜 데이터 생성기 (docs/DATA_SCHEMA.md 형식). UI 테스트용.
//   node testdata/gen-fake.js [--vcs 15] [--vms 13000] [--hosts 700] [--out testdata/fake]
//                             [--fails 1] [--nodata 0] [--stale-hours 0] [--seed 1]
//   --fails N        마지막 수집이 fail 인 vCenter 수 (직전 성공 데이터는 유지)
//   --nodata N       fail + 성공 이력 없음(data 파일 없음) vCenter 수
//   --stale-hours H  manifest.generated 를 H 시간 전으로 (배너 테스트)
const fs = require('fs');
const path = require('path');

const args = { vcs: 15, vms: 13000, hosts: 700, out: path.join(__dirname, 'fake'), fails: 1, nodata: 0, staleHours: 0, seed: 1 };
for (let i = 2; i < process.argv.length; i++) {
  const m = /^--([\w-]+)(?:=(.*))?$/.exec(process.argv[i]);
  if (!m) continue;
  const k = m[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase());
  let v = m[2];
  if (v === undefined) v = process.argv[++i];
  args[k] = isNaN(+v) ? v : +v;
}

let seed = args.seed >>> 0;
function rnd() { // mulberry32
  seed = (seed + 0x6D2B79F5) >>> 0;
  let t = seed;
  t = Math.imul(t ^ (t >>> 15), t | 1);
  t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const ri = (a, b) => a + Math.floor(rnd() * (b - a + 1));
const pick = (arr) => arr[Math.floor(rnd() * arr.length)];
const pad = (n, w) => String(n).padStart(w, '0');
const uuid = () => 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => { const r = ri(0, 15); return (c === 'x' ? r : (r & 3) | 8).toString(16); });
const typeRank = { Folder: 0, ClusterComputeResource: 1, HostSystem: 2, VirtualMachine: 3 };

const now = new Date();
const generated = new Date(now.getTime() - args.staleHours * 3600e3);
const iso = (d) => { // RFC3339 (+09:00 고정)
  const k = new Date(d.getTime() + 9 * 3600e3);
  return k.toISOString().replace(/\.\d+Z$/, '+09:00');
};

const APPS = ['web', 'was', 'db', 'batch', 'hpc', 'gpu', 'dev', 'test', 'ci', 'mon', 'log', 'ldap', 'nfs', 'lic'];
const GUEST = [['Red Hat Enterprise Linux 8 (64-bit)', 'rhel8_64Guest'], ['Rocky Linux (64-bit)', 'rockylinux_64Guest'],
  ['Microsoft Windows Server 2019 (64-bit)', 'windows2019srvNext_64Guest'], ['Ubuntu Linux (64-bit)', 'ubuntu64Guest'],
  ['Other 3.x or later Linux (64-bit)', 'other3xLinux64Guest']];

function genVc(idx, nHosts, nVms, opts) {
  const id = 'vc' + pad(idx + 1, 2);
  const name = id + '.corp.local';
  const url = 'https://' + name;
  const instanceUuid = uuid();
  const objects = {};
  const cnt = { dc: 2, grp: 4, c: 8, host: 11, vm: 100 };
  const nid = (p, k) => p + '-' + (cnt[k]++);
  const collectedAt = opts.stale ? iso(new Date(now.getTime() - 30 * 3600e3)) : iso(generated);

  const datastores = {}, networks = {};
  const nDs = ri(3, 6);
  for (let d = 0; d < nDs; d++) datastores['datastore-' + (15 + d)] = { name: `${id}-ds${d + 1}`, type: pick(['VMFS', 'NFS', 'vsan']), capacity: ri(2, 40) * 1099511627776, free: ri(1, 20) * 1099511627776 };
  for (let n = 0; n < 4; n++) networks['network-' + (17 + n)] = { name: pick(['VM Network', 'mgmt-vlan10', 'prod-vlan20', 'hpc-vlan30']) + (n ? '-' + n : '') };
  const dsIds = Object.keys(datastores), nwIds = Object.keys(networks);

  const dcCount = idx % 3 === 0 ? 2 : 1;
  const rootChildren = [];
  const hostList = [];
  let hostSeq = 0;

  function addHost(parent) {
    const hid = nid('host', 'host');
    hostSeq++;
    const st = rnd();
    const conn = st < 0.03 ? 'disconnected' : st < 0.04 ? 'notResponding' : 'connected';
    const maint = conn === 'connected' && rnd() < 0.03;
    const hname = rnd() < 0.5 ? `10.${idx + 1}.${Math.floor(hostSeq / 250)}.${(hostSeq % 250) + 1}` : `${id}-esx${pad(hostSeq, 3)}.corp.local`;
    const cores = pick([16, 24, 32, 48]);
    objects[hid] = {
      type: 'HostSystem', name: hname, parent, children: [], connectionState: conn,
      powerState: conn === 'connected' ? 'poweredOn' : 'unknown', inMaintenance: maint, overallStatus: conn === 'connected' ? 'green' : 'gray',
      vendor: 'Dell Inc.', model: 'PowerEdge R750', cpuModel: 'Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz', cpuSockets: 2, cpuCores: cores, cpuThreads: cores * 2,
      cpuMhzTotal: cores * 2000, cpuMhzUsed: ri(500, cores * 900), memTotalMB: pick([262144, 524288, 1048576]), memUsedMB: ri(20000, 200000),
      esxVersion: '8.0.3', esxBuild: '24280767', esxFullName: 'VMware ESXi 8.0.3 build-24280767',
      bootTime: iso(new Date(now.getTime() - ri(1, 90) * 86400e3)), uptimeSec: ri(1, 90) * 86400,
      datastores: dsIds.slice(0, 3), networks: nwIds.slice(0, 2),
    };
    hostList.push(hid);
    return hid;
  }

  const dcHostsQuota = Array.from({ length: dcCount }, (_, k) => Math.floor(nHosts / dcCount) + (k === 0 ? nHosts % dcCount : 0));
  for (let d = 0; d < dcCount; d++) {
    const did = nid('datacenter', 'dc');
    const dcName = dcCount > 1 ? `DC-${d + 1}` : 'HPC';
    objects[did] = { type: 'Datacenter', name: dcName, parent: '', children: [] };
    rootChildren.push(did);
    let left = dcHostsQuota[d];
    const folders = [];
    const nf = ri(0, 2);
    for (let f = 0; f < nf; f++) {
      const fid = nid('group-h', 'grp');
      objects[fid] = { type: 'Folder', name: `Team-${String.fromCharCode(65 + f)}`, parent: did, children: [] };
      objects[did].children.push(fid);
      folders.push(fid);
    }
    if (nf > 0 && rnd() < 0.5) {
      const fid = nid('group-h', 'grp');
      objects[fid] = { type: 'Folder', name: 'Archive', parent: folders[0], children: [] };
      objects[folders[0]].children.push(fid);
      folders.push(fid);
    }
    const standalone = Math.min(left, Math.max(0, Math.round(left * 0.08)));
    left -= standalone;
    while (left > 0) {
      const sz = Math.min(left, ri(4, 16));
      const parent = folders.length && rnd() < 0.6 ? pick(folders) : did;
      const cid = nid('domain-c', 'c');
      const cl = { type: 'ClusterComputeResource', name: `${pick(['HPC', 'Prod', 'Dev', 'GPU', 'VDI'])}-Cluster-${cnt.c}`, parent, children: [], numHosts: sz, numEffectiveHosts: sz,
        drsEnabled: rnd() < 0.7, haEnabled: rnd() < 0.7, overallStatus: 'green', datastores: dsIds.slice(0, 3), networks: nwIds.slice(0, 2) };
      objects[cid] = cl;
      objects[parent].children.push(cid);
      for (let h = 0; h < sz; h++) cl.children.push(addHost(cid));
      left -= sz;
    }
    for (let h = 0; h < standalone; h++) {
      const parent = folders.length && rnd() < 0.3 ? pick(folders) : did;
      objects[parent].children.push(addHost(parent));
    }
  }
  for (const c of Object.values(objects)) {
    if (c.type !== 'ClusterComputeResource') continue;
    c.totalCpuMhz = c.usedCpuMhz = c.totalMemMB = c.usedMemMB = 0;
    for (const h of c.children) { const o = objects[h]; c.totalCpuMhz += o.cpuMhzTotal; c.usedCpuMhz += o.cpuMhzUsed; c.totalMemMB += o.memTotalMB; c.usedMemMB += o.memUsedMB; }
    c.numHosts = c.numEffectiveHosts = c.children.length;
  }
  const index = [];
  let vmCount = 0;
  for (let v = 0; v < nVms && hostList.length; v++) {
    const hid = pick(hostList);
    const h = objects[hid];
    const vid = nid('vm', 'vm');
    const vname = `${pick(APPS)}-${pad(v + 1, 4)}`;
    const r = rnd();
    let power = r < 0.85 ? 'poweredOn' : r < 0.97 ? 'poweredOff' : 'suspended';
    if (h.inMaintenance && rnd() < 0.7) power = 'poweredOff';
    const conn = h.connectionState === 'connected' ? 'connected' : 'disconnected';
    if (conn !== 'connected') power = 'poweredOn';
    const ips = power === 'poweredOn' && conn === 'connected' ? [`10.${idx + 1}.${ri(0, 255)}.${ri(1, 254)}`].concat(rnd() < 0.2 ? [`172.16.${ri(0, 255)}.${ri(1, 254)}`] : []) : [];
    const g = pick(GUEST);
    const ghn = ips.length ? `${vname}.corp.local` : '';
    const numCpu = pick([1, 2, 4, 8, 16]);
    const memMB = pick([2048, 4096, 8192, 16384, 65536]);
    objects[vid] = {
      type: 'VirtualMachine', name: vname, parent: hid, host: hid, powerState: power, connectionState: conn, overallStatus: 'green', template: false,
      guestFullName: g[0], guestId: g[1], guestHostName: ghn, ips, toolsRunning: power === 'poweredOn' ? 'guestToolsRunning' : 'guestToolsNotRunning',
      toolsVersionStatus: 'guestToolsCurrent', toolsVersion: '12389', numCpu, memMB, cpuUsageMhz: power === 'poweredOn' ? ri(0, 2000) : 0,
      guestMemUsageMB: power === 'poweredOn' ? ri(100, memMB) : 0, hostMemUsageMB: power === 'poweredOn' ? ri(100, memMB) : 0,
      storageCommitted: ri(10, 500) * 1073741824, storageUncommitted: ri(0, 100) * 1073741824, hwVersion: 'vmx-19', annotation: '',
      disks: [{ label: 'Hard disk 1', capacityBytes: ri(20, 500) * 1073741824, thin: rnd() < 0.5, datastore: dsIds[0], file: `[${datastores[dsIds[0]].name}] ${vname}/${vname}.vmdk` }],
      nics: [{ label: 'Network adapter 1', network: networks[nwIds[0]].name, mac: '00:50:56:' + [1, 2, 3].map(() => pad(ri(0, 255).toString(16), 2)).join(':'), connected: power === 'poweredOn' }],
      datastores: [dsIds[0]], networks: [nwIds[0]],
    };
    h.children.push(vid);
    vmCount++;
    index.push([id, 'V', vid, vname, ips.join(' '), h.name, ghn]);
  }
  for (const o of Object.values(objects)) {
    if (o.children) o.children.sort((a, b) => (typeRank[objects[a].type] - typeRank[objects[b].type]) || objects[a].name.localeCompare(objects[b].name, 'en', { numeric: true }));
  }
  rootChildren.sort((a, b) => objects[a].name.localeCompare(objects[b].name));
  let clusters = 0;
  for (const [oid, o] of Object.entries(objects)) {
    if (o.type === 'HostSystem') index.push([id, 'H', oid, o.name, '', '', '']);
    else if (o.type === 'ClusterComputeResource') { clusters++; index.push([id, 'C', oid, o.name, '', '', '']); }
  }
  const data = { id, name, url, instanceUuid, version: '8.0.3', build: '24322831', collectedAt, rootChildren, objects, datastores, networks };
  const meta = { id, name, url, instanceUuid, version: '8.0.3', build: '24322831', status: 'ok', error: '', collectedAt, counts: { hosts: hostList.length, vms: vmCount, clusters } };
  return { data, meta, index };
}

const dataDir = path.join(args.out, 'data');
fs.mkdirSync(dataDir, { recursive: true });
const manifest = { generated: iso(generated), vcenters: [] };
const allIndex = [];
const ERR = 'ServerFaultCode: Cannot complete login due to an incorrect user name or password.';
for (let i = 0; i < args.vcs; i++) {
  const hosts = Math.max(1, Math.round(args.hosts / args.vcs * (0.6 + rnd() * 0.8)));
  const vms = Math.max(0, Math.round(args.vms / args.vcs * (0.6 + rnd() * 0.8)));
  const failIdx = i >= 6 && i < 6 + args.fails;
  const nodataIdx = i >= args.vcs - args.nodata;
  if (nodataIdx) {
    const id = 'vc' + pad(i + 1, 2);
    manifest.vcenters.push({ id, name: id + '.corp.local', url: `https://${id}.corp.local`, instanceUuid: '', version: '8.0.3', build: '24322831', status: 'fail', error: 'dial tcp: i/o timeout', collectedAt: '', counts: { hosts: 0, vms: 0, clusters: 0 } });
    continue;
  }
  const r = genVc(i, hosts, vms, { stale: failIdx });
  if (failIdx) { r.meta.status = 'fail'; r.meta.error = ERR; }
  manifest.vcenters.push(r.meta);
  allIndex.push(...r.index);
  fs.writeFileSync(path.join(dataDir, r.meta.id + '.js'),
    `window.VCP_DATA = window.VCP_DATA || {};\nwindow.VCP_DATA[${JSON.stringify(r.meta.id)}] = ${JSON.stringify(r.data)};\n`);
}
fs.writeFileSync(path.join(dataDir, 'manifest.js'), 'window.VCP_MANIFEST = ' + JSON.stringify(manifest, null, 1) + ';\n');
fs.writeFileSync(path.join(dataDir, 'index.js'), 'window.VCP_INDEX = [\n' + allIndex.map((e) => JSON.stringify(e)).join(',\n') + '\n];\n');
const tot = manifest.vcenters.reduce((a, v) => ({ h: a.h + v.counts.hosts, v: a.v + v.counts.vms }), { h: 0, v: 0 });
console.log(`generated ${manifest.vcenters.length} vCenters, ${tot.h} hosts, ${tot.v} VMs, ${allIndex.length} index entries -> ${dataDir}`);
