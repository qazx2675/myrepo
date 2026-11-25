#!/usr/bin/env python
# -*- coding: utf-8 -*-
from __future__ import print_function, unicode_literals

import json
import os
import re
import sys

PY2 = (sys.version_info[0] == 2)
if PY2:
    TEXT_TYPE = unicode  # noqa: F821
    BYTE_TYPE = str
    INT_TYPES = (int, long)  # noqa: F821
else:
    TEXT_TYPE = str
    BYTE_TYPE = bytes
    INT_TYPES = (int,)


REMEDIATION_FORCE_DELETE = (
    "기존 RAID 볼륨에 할당된 디스크가 감지되었습니다. "
    "기존 볼륨을 삭제하고 재생성하려면 --force-delete 옵션을 사용하세요: "
    "bash raid0.sh --force-delete"
)

RAID_TO_SSC = {
    "0": "Raid0",
    "1": "Raid1",
    "5": "Raid5",
    "6": "Raid6",
    "10": "Raid10",
    "50": "Raid50",
    "60": "Raid60",
}

RAID_TO_REDFISH = {
    "0": "RAID0",
    "1": "RAID1",
    "5": "RAID5",
    "6": "RAID6",
    "10": "RAID10",
    "50": "RAID50",
    "60": "RAID60",
}


def to_text(val):
    if val is None:
        return TEXT_TYPE("")
    if isinstance(val, TEXT_TYPE):
        return val
    if isinstance(val, BYTE_TYPE):
        return val.decode("utf-8", "replace")
    return TEXT_TYPE(val)


def out_print(text):
    line = to_text(text) + "\n"
    if PY2:
        sys.stdout.write(line.encode("utf-8"))
        sys.stdout.flush()
    else:
        if hasattr(sys.stdout, "buffer"):
            sys.stdout.buffer.write(line.encode("utf-8"))
            sys.stdout.buffer.flush()
        else:
            sys.stdout.write(line)
            sys.stdout.flush()


def err_print(text):
    line = to_text(text) + "\n"
    if PY2:
        sys.stderr.write(line.encode("utf-8"))
        sys.stderr.flush()
    else:
        if hasattr(sys.stderr, "buffer"):
            sys.stderr.buffer.write(line.encode("utf-8"))
            sys.stderr.buffer.flush()
        else:
            sys.stderr.write(line)
            sys.stderr.flush()


def read_text_file(path):
    if not path or not os.path.isfile(path):
        return None
    with open(path, "rb") as f:
        raw = f.read()
    return raw.decode("utf-8", "replace")


def write_text_file(path, text):
    parent = os.path.dirname(path)
    if parent and not os.path.isdir(parent):
        os.makedirs(parent)
    data = to_text(text).replace("\r\n", "\n").replace("\r", "\n").encode("utf-8")
    with open(path, "wb") as f:
        f.write(data)


def load_json_file(path):
    text = read_text_file(path)
    if text is None:
        return None
    stripped = text.strip()
    if not stripped:
        return None
    try:
        return json.loads(stripped)
    except ValueError:
        pass
    # Fallback: ilorest rawget may include banner/status lines around JSON
    obj_start = stripped.find("{")
    obj_end = stripped.rfind("}")
    if obj_start != -1 and obj_end != -1 and obj_end > obj_start:
        try:
            return json.loads(stripped[obj_start:obj_end + 1])
        except ValueError:
            pass
    arr_start = stripped.find("[")
    arr_end = stripped.rfind("]")
    if arr_start != -1 and arr_end != -1 and arr_end > arr_start:
        try:
            return json.loads(stripped[arr_start:arr_end + 1])
        except ValueError:
            pass
    return None


def dump_json_file(path, obj):
    text = to_text(json.dumps(obj, indent=2, sort_keys=True, ensure_ascii=False))
    write_text_file(path, text + "\n")


def get_nested(obj, keys, default=None):
    cur = obj
    for k in keys:
        if isinstance(cur, dict) and k in cur:
            cur = cur[k]
        else:
            return default
    return cur


def safe_ip_name(ip):
    return re.sub(r"[.:]", "_", to_text(ip).strip())


def ip_sort_key(ip):
    ip_s = to_text(ip).strip()
    parts = ip_s.split(".")
    if len(parts) == 4:
        nums = []
        ok = True
        for p in parts:
            if p.isdigit():
                nums.append(int(p))
            else:
                ok = False
                break
        if ok:
            return (0, tuple(nums), ip_s)
    return (1, (), ip_s)


def natural_sort_key(text):
    s = to_text(text)
    tokens = re.split(r"(\d+)", s)
    key = []
    for tok in tokens:
        if not tok:
            continue
        if tok.isdigit():
            key.append((0, int(tok)))
        else:
            key.append((1, tok.lower()))
    return tuple(key)


def norm_uri(uri):
    return to_text(uri).strip().rstrip("/")


def norm_loc(loc):
    return re.sub(r"\s+", "", to_text(loc).strip().lower())


def format_capacity_human(cap_bytes):
    if not cap_bytes or cap_bytes <= 0:
        return "0B"
    tb = 1000.0 * 1000.0 * 1000.0 * 1000.0
    gb = 1000.0 * 1000.0 * 1000.0
    mb = 1000.0 * 1000.0
    if cap_bytes >= tb:
        return "%.1fTB" % (float(cap_bytes) / tb)
    if cap_bytes >= gb:
        return "%dGB" % int(round(float(cap_bytes) / gb))
    if cap_bytes >= mb:
        return "%dMB" % int(round(float(cap_bytes) / mb))
    return "%dB" % int(cap_bytes)


def pad_display_width(text, width):
    # Approximate display width accounting for East Asian wide chars (Hangul)
    s = to_text(text)
    disp = 0
    for ch in s:
        code = ord(ch)
        if (0x1100 <= code <= 0x11FF or
                0x3000 <= code <= 0x9FFF or
                0xAC00 <= code <= 0xD7AF or
                0xFF00 <= code <= 0xFF60):
            disp += 2
        else:
            disp += 1
    if disp >= width:
        return s
    return s + (" " * (width - disp))


def format_table(headers, rows):
    # compute max display widths
    def dlen(val):
        s = to_text(val)
        w = 0
        for ch in s:
            code = ord(ch)
            if (0x1100 <= code <= 0x11FF or
                    0x3000 <= code <= 0x9FFF or
                    0xAC00 <= code <= 0xD7AF or
                    0xFF00 <= code <= 0xFF60):
                w += 2
            else:
                w += 1
        return w

    widths = [dlen(h) for h in headers]
    for row in rows:
        for idx in range(len(headers)):
            cell = row[idx] if idx < len(row) else ""
            w = dlen(cell)
            if w > widths[idx]:
                widths[idx] = w

    lines = []
    hdr_parts = []
    sep_parts = []
    for idx in range(len(headers)):
        hdr_parts.append(pad_display_width(headers[idx], widths[idx]))
        sep_parts.append("-" * widths[idx])
    lines.append(" | ".join(hdr_parts))
    lines.append("-+-".join(sep_parts))
    for row in rows:
        row_parts = []
        for idx in range(len(headers)):
            cell = row[idx] if idx < len(row) else ""
            row_parts.append(pad_display_width(cell, widths[idx]))
        lines.append(" | ".join(row_parts))
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Subcommand 1: extract-links
# ---------------------------------------------------------------------------

def _extract_odata_uris(val):
    uris = []
    if isinstance(val, dict):
        u = val.get("@odata.id")
        if u:
            uris.append(to_text(u))
    elif isinstance(val, list):
        for item in val:
            if isinstance(item, dict) and item.get("@odata.id"):
                uris.append(to_text(item["@odata.id"]))
            elif isinstance(item, TEXT_TYPE) and item.startswith("/"):
                uris.append(item)
    return uris


def _check_reboot_indicator(data, raw_text):
    # First inspect structured Redfish messages if valid JSON
    if isinstance(data, dict):
        ext_lists = []
        if isinstance(data.get("@Message.ExtendedInfo"), list):
            ext_lists.append(data.get("@Message.ExtendedInfo"))
        if isinstance(data.get("Messages"), list):
            ext_lists.append(data.get("Messages"))
        err_ext = get_nested(data, ["error", "@Message.ExtendedInfo"])
        if isinstance(err_ext, list):
            ext_lists.append(err_ext)
        for ext in ext_lists:
            for msg in ext:
                if isinstance(msg, dict):
                    mid = to_text(msg.get("MessageId", ""))
                    mtxt = to_text(msg.get("Message", ""))
                    combined = mid + " " + mtxt
                    if re.search(r"SystemResetRequired|RebootRequired|ResetRequired", combined, re.I):
                        return True
                    if re.search(r"\b(reboot|reset)\b", combined, re.I):
                        if not re.search(r"\b(no\s+reboot|not\s+required|false)\b", combined, re.I):
                            return True
                elif isinstance(msg, TEXT_TYPE):
                    if re.search(r"SystemResetRequired|RebootRequired|reboot|reset", msg, re.I):
                        return True
        # Check explicit boolean/string fields
        for k in ("SystemResetRequired", "RebootRequired", "PendingReset"):
            v = data.get(k)
            if v is True or to_text(v).lower() in ("true", "yes", "required", "pending"):
                return True
        msg_str = to_text(data.get("Message", ""))
        if msg_str and re.search(r"SystemResetRequired|reboot|reset", msg_str, re.I):
            if not re.search(r"\b(no\s+reboot|not\s+required)\b", msg_str, re.I):
                return True

    # Fallback raw text scan
    if raw_text:
        if re.search(r"SystemResetRequired|RebootRequired", raw_text, re.I):
            if not re.search(r"SystemResetRequired[\"']?\s*[:=]\s*false", raw_text, re.I):
                return True
        if re.search(r"(reboot|reset).{0,40}(required|pending|needed|apply|effect)", raw_text, re.I):
            if not re.search(r"not\s+required|no\s+reboot", raw_text, re.I):
                return True
        if re.search(r"(required|pending).{0,40}(reboot|reset)", raw_text, re.I):
            return True
    return False


def cmd_extract_links(args):
    if len(args) < 2:
        err_print("Usage: extract-links <json_file> <mode>")
        return 1
    json_path = args[0]
    mode = args[1]

    if mode == "check_reboot_msg":
        raw_text = read_text_file(json_path) or ""
        data = load_json_file(json_path)
        if _check_reboot_indicator(data, raw_text):
            out_print("REBOOT")
        else:
            out_print("APPLIED")
        return 0

    data = load_json_file(json_path)
    if not isinstance(data, dict):
        return 0

    results = []
    if mode == "members":
        results = _extract_odata_uris(data.get("Members"))
    elif mode == "drives":
        drives = data.get("Drives")
        if drives is None:
            drives = get_nested(data, ["Links", "Drives"])
        if isinstance(drives, dict) and drives.get("@odata.id"):
            results = ["COLLECTION:" + to_text(drives["@odata.id"])]
        elif isinstance(drives, list):
            results = _extract_odata_uris(drives)
    elif mode == "volumes_uri":
        v = data.get("Volumes")
        if isinstance(v, dict) and v.get("@odata.id"):
            results = [to_text(v["@odata.id"])]
    elif mode == "capabilities_uri":
        cap = data.get("Capabilities")
        if isinstance(cap, dict) and cap.get("@odata.id"):
            results = [to_text(cap["@odata.id"])]
        else:
            v = data.get("Volumes")
            if isinstance(v, dict) and v.get("@odata.id"):
                results = [norm_uri(v["@odata.id"]) + "/Capabilities"]
    elif mode == "settings_uri":
        su = get_nested(data, ["@Redfish.Settings", "SettingsObject", "@odata.id"])
        if su:
            results = [to_text(su)]
    elif mode == "ssc_drive_links":
        for candidate in (
            get_nested(data, ["Links", "PhysicalDrives"]),
            get_nested(data, ["Links", "DiskDrives"]),
            data.get("PhysicalDrives"),
            data.get("DiskDrives"),
        ):
            uris = _extract_odata_uris(candidate)
            if uris:
                results = uris
                break
    elif mode == "ssc_volume_links":
        for candidate in (
            get_nested(data, ["Links", "LogicalDrives"]),
            data.get("LogicalDrives"),
        ):
            uris = _extract_odata_uris(candidate)
            if uris:
                results = uris
                break
    elif mode == "ssc_config_uri":
        for path_keys in (
            ["Oem", "Hpe", "Links", "SmartStorageConfig", "@odata.id"],
            ["Oem", "Hp", "Links", "SmartStorageConfig", "@odata.id"],
            ["Oem", "Hpe", "SmartStorageConfig", "@odata.id"],
        ):
            u = get_nested(data, path_keys)
            if u:
                results = [to_text(u)]
                break

    for line in results:
        if line:
            out_print(line)
    return 0


# ---------------------------------------------------------------------------
# Subcommand 2: normalize
# ---------------------------------------------------------------------------

def _detect_generation(model, fw_version):
    m = to_text(model)
    fw = to_text(fw_version)
    if re.search(r"gen\s*11", m, re.I) or re.search(r"ilo\s*6", fw, re.I):
        return "Gen11"
    if re.search(r"gen\s*10\s*(plus|\+)", m, re.I):
        return "Gen10Plus"
    if re.search(r"gen\s*10", m, re.I):
        return "Gen10"
    return "Unknown"


def _is_boot_controller(model, name, ctrl_id):
    combined = ("%s %s %s" % (to_text(model), to_text(name), to_text(ctrl_id))).lower()
    for kw in ("ns204", "boss", "boot"):
        if kw in combined:
            return True
    return False


def _is_rde_or_mr_controller(model, name, odata_id):
    combined = ("%s %s" % (to_text(model), to_text(name))).lower()
    if re.search(r"\bmr\d+|megaraid|sr932|sr416|ns204", combined):
        return True
    if re.search(r"/DE0[0-9A-Fa-f]+", to_text(odata_id)):
        return True
    return False


def _extract_location(obj):
    if not isinstance(obj, dict):
        return ""
    loc = obj.get("Location")
    if isinstance(loc, TEXT_TYPE) and loc.strip():
        return loc.strip()
    if isinstance(loc, list):
        for item in loc:
            if isinstance(item, dict):
                info = item.get("Info")
                if info:
                    return to_text(info).strip()
                pl = get_nested(item, ["PartLocation", "ServiceLabel"])
                if pl:
                    return to_text(pl).strip()
    if isinstance(loc, dict):
        info = loc.get("Info")
        if info:
            return to_text(info).strip()
        pl = get_nested(loc, ["PartLocation", "ServiceLabel"])
        if pl:
            return to_text(pl).strip()

    for keys in (
        ["PhysicalLocation", "PartLocation", "ServiceLabel"],
        ["PhysicalLocation", "Info"],
        ["Oem", "Hpe", "Location"],
        ["Oem", "Hp", "Location"],
    ):
        val = get_nested(obj, keys)
        if val:
            return to_text(val).strip()

    bay = obj.get("Bay")
    if bay is not None and to_text(bay).strip():
        bstr = to_text(bay).strip()
        if bstr.isdigit():
            return "Bay %s" % bstr
        return bstr

    name = obj.get("Name")
    if name:
        return to_text(name).strip()
    return to_text(obj.get("Id", "")).strip()


def _normalize_protocol(raw_proto):
    p = to_text(raw_proto).upper()
    if "NVME" in p or "PCIE" in p:
        return "NVMe"
    if "SAS" in p:
        return "SAS"
    if "SATA" in p:
        return "SATA"
    return "UNKNOWN"


def _normalize_media_type(raw_media):
    m = to_text(raw_media).strip()
    ml = m.lower()
    if not m:
        return "Unknown"
    if "ssd" in ml or "solid" in ml or "flash" in ml:
        return "SSD"
    if "hdd" in ml or "rotational" in ml or "hard" in ml:
        return "HDD"
    return m


def _normalize_drive_obj(d):
    cap_bytes = d.get("CapacityBytes")
    if not isinstance(cap_bytes, INT_TYPES) or cap_bytes <= 0:
        cap_gb = d.get("CapacityGB")
        cap_mib = d.get("CapacityMiB")
        if isinstance(cap_gb, (INT_TYPES, float)) and cap_gb > 0:
            cap_bytes = int(float(cap_gb) * 1000.0 * 1000.0 * 1000.0)
        elif isinstance(cap_mib, (INT_TYPES, float)) and cap_mib > 0:
            cap_bytes = int(float(cap_mib) * 1024.0 * 1024.0)
        else:
            cap_bytes = 0

    proto_raw = d.get("Protocol") or d.get("InterfaceType") or ""
    media_raw = d.get("MediaType") or ""

    status_obj = d.get("Status")
    status_str = "OK"
    if isinstance(status_obj, dict):
        health = status_obj.get("Health")
        state = status_obj.get("State")
        if health:
            status_str = to_text(health)
        elif state:
            status_str = to_text(state)
    elif isinstance(status_obj, TEXT_TYPE) and status_obj:
        status_str = status_obj

    link_vols = _extract_odata_uris(get_nested(d, ["Links", "Volumes"]))

    return {
        "index": 0,
        "id": to_text(d.get("Id", "")),
        "odata_id": to_text(d.get("@odata.id", "")),
        "location": _extract_location(d),
        "protocol": _normalize_protocol(proto_raw),
        "media_type": _normalize_media_type(media_raw),
        "capacity_bytes": int(cap_bytes),
        "capacity_human": format_capacity_human(cap_bytes),
        "status": status_str,
        "assigned_volumes": [],
        "_link_volumes": link_vols,
    }


def _normalize_volume_obj(v, fallback_idx=0):
    vid = to_text(v.get("Id", ""))
    ld_num = v.get("LogicalDriveNumber")
    if not vid and ld_num is not None:
        vid = to_text(ld_num)
    if not vid:
        vid = to_text(fallback_idx)

    name = (
        v.get("DisplayName")
        or v.get("LogicalDriveName")
        or v.get("Name")
        or ("Volume_%s" % vid)
    )
    raid_type = v.get("RAIDType") or v.get("Raid") or v.get("VolumeType") or ""

    cap_bytes = v.get("CapacityBytes")
    if not isinstance(cap_bytes, INT_TYPES) or cap_bytes <= 0:
        cap_mib = v.get("CapacityMiB")
        cap_gb = v.get("CapacityGB")
        if isinstance(cap_mib, (INT_TYPES, float)) and cap_mib > 0:
            cap_bytes = int(float(cap_mib) * 1024.0 * 1024.0)
        elif isinstance(cap_gb, (INT_TYPES, float)) and cap_gb > 0:
            cap_bytes = int(float(cap_gb) * 1000.0 * 1000.0 * 1000.0)
        else:
            cap_bytes = 0

    drive_refs = []
    # Redfish Storage Volume Links.Drives
    for uri in _extract_odata_uris(get_nested(v, ["Links", "Drives"])):
        if uri not in drive_refs:
            drive_refs.append(uri)
    # Direct Drives list
    for uri in _extract_odata_uris(v.get("Drives")):
        if uri not in drive_refs:
            drive_refs.append(uri)
    # SmartStorageConfig DataDrives (list of location strings like "1I:1:9")
    dd = v.get("DataDrives")
    if isinstance(dd, list):
        for item in dd:
            if isinstance(item, TEXT_TYPE) and item.strip():
                if item.strip() not in drive_refs:
                    drive_refs.append(item.strip())
            elif isinstance(item, dict) and item.get("@odata.id"):
                u = to_text(item["@odata.id"])
                if u not in drive_refs:
                    drive_refs.append(u)

    vol = {
        "id": vid,
        "name": to_text(name),
        "odata_id": to_text(v.get("@odata.id", "")),
        "raid_type": to_text(raid_type),
        "capacity_bytes": int(cap_bytes),
        "drive_refs": drive_refs,
    }
    if ld_num is not None:
        vol["logical_drive_number"] = ld_num
    if v.get("UniqueIdentifier"):
        vol["unique_identifier"] = to_text(v.get("UniqueIdentifier"))
    return vol


def _link_drives_and_volumes(drives, volumes):
    # Sort drives naturally by location and assign 1-based index
    drives.sort(key=lambda d: (natural_sort_key(d.get("location", "")), natural_sort_key(d.get("id", ""))))
    for idx, d in enumerate(drives):
        d["index"] = idx + 1

    # Map volume odata_id -> volume dict
    vol_by_uri = {}
    for v in volumes:
        vu = norm_uri(v.get("odata_id", ""))
        if vu:
            vol_by_uri[vu] = v

    # Incorporate drive._link_volumes into volumes' drive_refs
    for d in drives:
        du = norm_uri(d.get("odata_id", ""))
        for vu_raw in d.get("_link_volumes", []):
            vu = norm_uri(vu_raw)
            v = vol_by_uri.get(vu)
            if v is not None:
                if du and du not in [norm_uri(x) for x in v["drive_refs"]]:
                    v["drive_refs"].append(d["odata_id"])
                label = v["name"] or v["id"]
                if label and label not in d["assigned_volumes"]:
                    d["assigned_volumes"].append(label)
            else:
                # Volume wasn't fetched separately, still record URI basename as assigned
                label = vu.split("/")[-1] if vu else vu_raw
                if label and label not in d["assigned_volumes"]:
                    d["assigned_volumes"].append(label)

    # Map volume drive_refs -> drive.assigned_volumes
    for v in volumes:
        label = v.get("name") or v.get("id")
        ref_uris = [norm_uri(r) for r in v.get("drive_refs", []) if r]
        ref_locs = [norm_loc(r) for r in v.get("drive_refs", []) if r]
        for d in drives:
            du = norm_uri(d.get("odata_id", ""))
            dl = norm_loc(d.get("location", ""))
            matched = False
            if du and du in ref_uris:
                matched = True
            elif dl and dl in ref_locs:
                matched = True
            if matched and label and label not in d["assigned_volumes"]:
                d["assigned_volumes"].append(label)

    # Remove internal helper key
    for d in drives:
        if "_link_volumes" in d:
            del d["_link_volumes"]


def _list_indexed_files(raw_dir, pattern):
    regex = re.compile(pattern)
    matched = []
    if not os.path.isdir(raw_dir):
        return matched
    for fname in os.listdir(raw_dir):
        m = regex.match(fname)
        if m:
            matched.append((int(m.group(1)), os.path.join(raw_dir, fname)))
    matched.sort(key=lambda x: x[0])
    return matched


def _collect_ssc_settings_entries(raw_dir):
    entries = []
    # Check ssc_config_<k>.json + ssc_settings_<k>.json
    cfg_files = _list_indexed_files(raw_dir, r"^ssc_config_(\d+)\.json$")
    set_files = _list_indexed_files(raw_dir, r"^ssc_settings_(\d+)\.json$")

    set_map = {}
    for idx, fpath in set_files:
        data = load_json_file(fpath)
        if isinstance(data, dict):
            set_map[idx] = data

    cfg_map = {}
    for idx, fpath in cfg_files:
        data = load_json_file(fpath)
        if isinstance(data, dict):
            cfg_map[idx] = data

    # Also check ssc_config_root.json
    root_cfg = load_json_file(os.path.join(raw_dir, "ssc_config_root.json"))
    if isinstance(root_cfg, dict) and not cfg_map:
        cfg_map[0] = root_cfg

    all_indices = sorted(list(set(list(cfg_map.keys()) + list(set_map.keys()))))
    for idx in all_indices:
        c = cfg_map.get(idx) or {}
        s = set_map.get(idx) or {}
        s_uri = (
            s.get("@odata.id")
            or get_nested(c, ["@Redfish.Settings", "SettingsObject", "@odata.id"])
            or ""
        )
        if not s_uri and (s.get("LogicalDrives") is not None or s.get("DataGuard") is not None or s.get("PhysicalDrives") is not None):
            s_uri = "/redfish/v1/Systems/1/smartstorageconfig/settings/"
        if not s_uri and c.get("@odata.id") and not c.get("Members"):
            base = norm_uri(c.get("@odata.id"))
            if base:
                s_uri = base + "/settings/"
        if not s_uri:
            continue

        lds = s.get("LogicalDrives")
        if not isinstance(lds, list):
            lds = c.get("LogicalDrives")
        if not isinstance(lds, list):
            lds = []

        entries.append({
            "index": idx,
            "settings_uri": to_text(s_uri),
            "serial": to_text(s.get("SerialNumber") or c.get("SerialNumber") or ""),
            "location": to_text(s.get("Location") or c.get("Location") or c.get("ControllerLocation") or ""),
            "model": to_text(s.get("Model") or c.get("Model") or ""),
            "logical_drives": lds,
        })
    return entries


def _match_ssc_settings(ctrl_idx, ctrl_serial, ctrl_loc, ssc_entries):
    if not ssc_entries:
        return None
    if ctrl_serial:
        for e in ssc_entries:
            if e["serial"] and e["serial"].lower() == ctrl_serial.lower():
                return e
    if ctrl_loc:
        for e in ssc_entries:
            if e["location"] and norm_loc(e["location"]) == norm_loc(ctrl_loc):
                return e
    for e in ssc_entries:
        if e["index"] == ctrl_idx:
            return e
    if len(ssc_entries) == 1:
        return ssc_entries[0]
    return None


def _is_duplicate_storage_mirror(st_ctrl, ssc_ctrls):
    if _is_rde_or_mr_controller(st_ctrl["model"], st_ctrl["name"], st_ctrl["odata_id"]):
        return False
    if st_ctrl["is_boot"]:
        return False
    st_ser = st_ctrl.get("serial", "").strip().lower()
    st_loc = norm_loc(st_ctrl.get("location", ""))
    st_mod = st_ctrl.get("model", "").strip().lower()
    st_name = st_ctrl.get("name", "").strip().lower()

    for sc in ssc_ctrls:
        sc_ser = sc.get("serial", "").strip().lower()
        sc_loc = norm_loc(sc.get("location", ""))
        sc_mod = sc.get("model", "").strip().lower()
        if st_ser and sc_ser and st_ser == sc_ser:
            return True
        if st_loc and sc_loc and st_loc == sc_loc:
            return True
        if st_mod and sc_mod and (st_mod in sc_mod or sc_mod in st_mod):
            return True
        if st_name and sc_mod and (st_name in sc_mod or sc_mod in st_name):
            return True
        # Overlap by physical drive locations
        sc_locs = set([norm_loc(d["location"]) for d in sc.get("drives", []) if d.get("location")])
        st_locs = set([norm_loc(d["location"]) for d in st_ctrl.get("drives", []) if d.get("location")])
        if sc_locs and st_locs and len(sc_locs.intersection(st_locs)) > 0:
            return True
        # Gen10 read-only Smart Array /Storage stub without Volumes URI
        if not st_ctrl.get("volumes_uri"):
            if re.search(r"smart\s*array|p\d{3}i|e\d{3}i|s100i", st_mod + " " + st_name):
                return True
            if re.search(r"/DA\d+", st_ctrl.get("odata_id", ""), re.I):
                return True
    return False


def cmd_normalize(args):
    if len(args) < 3:
        err_print("Usage: normalize <ip> <raw_dir> <out_json>")
        return 1
    ip = to_text(args[0]).strip()
    raw_dir = args[1]
    out_json = args[2]

    err_txt = read_text_file(os.path.join(raw_dir, "error.txt"))
    if err_txt and err_txt.strip():
        result = {
            "ip": ip,
            "status": "ERROR",
            "error": err_txt.strip(),
            "model": "Unknown",
            "generation": "Unknown",
            "power_state": "Unknown",
            "post_state": "Unknown",
            "controllers": [],
        }
        dump_json_file(out_json, result)
        return 0

    system = load_json_file(os.path.join(raw_dir, "system.json"))
    if not isinstance(system, dict):
        result = {
            "ip": ip,
            "status": "ERROR",
            "error": "system.json 수집 실패 또는 잘못된 JSON",
            "model": "Unknown",
            "generation": "Unknown",
            "power_state": "Unknown",
            "post_state": "Unknown",
            "controllers": [],
        }
        dump_json_file(out_json, result)
        return 0

    manager = load_json_file(os.path.join(raw_dir, "manager.json"))
    if not isinstance(manager, dict):
        manager = {}

    model = to_text(system.get("Model") or system.get("Name") or "Unknown").strip()
    fw_version = to_text(manager.get("FirmwareVersion") or "")
    generation = _detect_generation(model, fw_version)
    power_state = to_text(system.get("PowerState") or "Unknown")
    post_state = to_text(
        get_nested(system, ["Oem", "Hpe", "PostState"])
        or get_nested(system, ["Oem", "Hp", "PostState"])
        or "Unknown"
    )

    ssc_entries = _collect_ssc_settings_entries(raw_dir)

    # 1. Parse SmartStorage ArrayControllers
    ssc_ctrls = []
    for idx, fpath in _list_indexed_files(raw_dir, r"^ssc_ctrl_(\d+)\.json$"):
        cdata = load_json_file(fpath)
        if not isinstance(cdata, dict):
            continue
        cid = to_text(cdata.get("Id") or ("ssc_%d" % idx))
        cmodel = to_text(cdata.get("Model") or cdata.get("Name") or cid).strip()
        cname = to_text(cdata.get("Name") or cmodel).strip()
        cserial = to_text(cdata.get("SerialNumber") or "").strip()
        cloc = _extract_location(cdata)
        codata = to_text(cdata.get("@odata.id") or "")

        is_boot = _is_boot_controller(cmodel, cname, cid)
        is_rde = _is_rde_or_mr_controller(cmodel, cname, codata)
        matched_set = _match_ssc_settings(idx, cserial, cloc, ssc_entries)

        if generation == "Gen11" or is_rde or matched_set is None:
            continue

        drives = []
        for _didx, dfpath in _list_indexed_files(raw_dir, r"^ssc_ctrl_%d_drive_(\d+)\.json$" % idx):
            ddata = load_json_file(dfpath)
            if isinstance(ddata, dict):
                drives.append(_normalize_drive_obj(ddata))

        volumes = []
        for vidx, vfpath in _list_indexed_files(raw_dir, r"^ssc_ctrl_%d_volume_(\d+)\.json$" % idx):
            vdata = load_json_file(vfpath)
            if isinstance(vdata, dict):
                volumes.append(_normalize_volume_obj(vdata, fallback_idx=vidx))

        # Enrich or synthesize volumes from smartstorageconfig/settings LogicalDrives
        settings_lds = matched_set.get("logical_drives") or []
        if settings_lds:
            if not volumes:
                for lidx, ld in enumerate(settings_lds):
                    if isinstance(ld, dict):
                        volumes.append(_normalize_volume_obj(ld, fallback_idx=lidx + 1))
            else:
                for ld in settings_lds:
                    if not isinstance(ld, dict):
                        continue
                    ld_num = ld.get("LogicalDriveNumber")
                    ld_name = to_text(ld.get("LogicalDriveName", ""))
                    ld_uid = to_text(ld.get("UniqueIdentifier", ""))
                    ld_drives = ld.get("DataDrives") or []
                    matched_vol = None
                    for v in volumes:
                        if ld_num is not None and (v.get("logical_drive_number") == ld_num or v.get("id") == to_text(ld_num)):
                            matched_vol = v
                            break
                        if ld_uid and v.get("unique_identifier") == ld_uid:
                            matched_vol = v
                            break
                        if ld_name and v.get("name") == ld_name:
                            matched_vol = v
                            break
                    if matched_vol is not None:
                        if isinstance(ld_drives, list):
                            for dref in ld_drives:
                                if isinstance(dref, TEXT_TYPE) and dref not in matched_vol["drive_refs"]:
                                    matched_vol["drive_refs"].append(dref)
                        if ld_num is not None and "logical_drive_number" not in matched_vol:
                            matched_vol["logical_drive_number"] = ld_num
                        if ld_uid and "unique_identifier" not in matched_vol:
                            matched_vol["unique_identifier"] = ld_uid
                    else:
                        volumes.append(_normalize_volume_obj(ld, fallback_idx=len(volumes) + 1))

        _link_drives_and_volumes(drives, volumes)
        ssc_ctrls.append({
            "index": 0,
            "id": cid,
            "name": cname,
            "model": cmodel,
            "serial": cserial,
            "location": cloc,
            "odata_id": codata,
            "backend": "smartstorageconfig",
            "is_boot": is_boot,
            "volumes_uri": "",
            "settings_uri": matched_set["settings_uri"],
            "supported_raids": ["0", "1", "5", "6", "10", "50", "60"],
            "drives": drives,
            "volumes": volumes,
        })

    # 2. Parse Redfish /Storage controllers
    storage_ctrls = []
    for idx, fpath in _list_indexed_files(raw_dir, r"^storage_(\d+)\.json$"):
        sdata = load_json_file(fpath)
        if not isinstance(sdata, dict):
            continue
        cid = to_text(sdata.get("Id") or ("storage_%d" % idx))
        sc_list = sdata.get("StorageControllers")
        sc0 = {}
        if isinstance(sc_list, list) and len(sc_list) > 0 and isinstance(sc_list[0], dict):
            sc0 = sc_list[0]
        elif isinstance(sc_list, dict):
            sc0 = sc_list

        cname = to_text(sdata.get("Name") or sc0.get("Name") or cid).strip()
        cmodel = to_text(sc0.get("Model") or sdata.get("Model") or sdata.get("Description") or cname).strip()
        cserial = to_text(sc0.get("SerialNumber") or sdata.get("SerialNumber") or "").strip()
        cloc = _extract_location(sc0) or _extract_location(sdata)
        codata = to_text(sdata.get("@odata.id") or "")
        vol_uri = to_text(get_nested(sdata, ["Volumes", "@odata.id"]) or "")
        is_boot = _is_boot_controller(cmodel, cname, cid)

        drives = []
        for _didx, dfpath in _list_indexed_files(raw_dir, r"^storage_%d_drive_(\d+)\.json$" % idx):
            ddata = load_json_file(dfpath)
            if isinstance(ddata, dict):
                drives.append(_normalize_drive_obj(ddata))

        volumes = []
        for vidx, vfpath in _list_indexed_files(raw_dir, r"^storage_%d_volume_(\d+)\.json$" % idx):
            vdata = load_json_file(vfpath)
            if isinstance(vdata, dict):
                volumes.append(_normalize_volume_obj(vdata, fallback_idx=vidx))

        supported_raids = ["0", "1", "5", "6", "10", "50", "60"]
        cap_data = load_json_file(os.path.join(raw_dir, "storage_%d_capabilities.json" % idx))
        if isinstance(cap_data, dict):
            allowable = cap_data.get("RAIDType@Redfish.AllowableValues")
            if isinstance(allowable, list) and allowable:
                parsed_raids = []
                for rv in allowable:
                    m = re.match(r"^(?:RAID|Raid)(\d+)$", to_text(rv).strip())
                    if m and m.group(1) in RAID_TO_REDFISH:
                        parsed_raids.append(m.group(1))
                if parsed_raids:
                    supported_raids = parsed_raids

        _link_drives_and_volumes(drives, volumes)
        ctrl_obj = {
            "index": 0,
            "id": cid,
            "name": cname,
            "model": cmodel,
            "serial": cserial,
            "location": cloc,
            "odata_id": codata,
            "backend": "redfish_storage",
            "is_boot": is_boot,
            "volumes_uri": vol_uri,
            "settings_uri": "",
            "supported_raids": supported_raids,
            "drives": drives,
            "volumes": volumes,
        }

        if not vol_uri and not drives:
            continue

        if generation != "Gen11" and _is_duplicate_storage_mirror(ctrl_obj, ssc_ctrls):
            continue

        storage_ctrls.append(ctrl_obj)

    controllers = []
    if generation == "Gen11":
        controllers = storage_ctrls
    else:
        controllers = ssc_ctrls + storage_ctrls

    for idx, c in enumerate(controllers):
        c["index"] = idx + 1

    result = {
        "ip": ip,
        "status": "OK",
        "error": "",
        "model": model,
        "generation": generation,
        "power_state": power_state,
        "post_state": post_state,
        "controllers": controllers,
    }
    dump_json_file(out_json, result)
    return 0


# ---------------------------------------------------------------------------
# Subcommand 3: summarize
# ---------------------------------------------------------------------------

def _load_inventories(inv_dir):
    invs = []
    if not os.path.isdir(inv_dir):
        return invs
    for fname in os.listdir(inv_dir):
        if fname.startswith("inv_") and fname.endswith(".json"):
            data = load_json_file(os.path.join(inv_dir, fname))
            if isinstance(data, dict) and data.get("ip"):
                invs.append(data)
    invs.sort(key=lambda x: ip_sort_key(x.get("ip", "")))
    return invs


def _proto_counts(drives):
    counts = {"SAS": 0, "SATA": 0, "NVMe": 0, "UNKNOWN": 0}
    for d in drives:
        p = d.get("protocol", "UNKNOWN")
        if p in counts:
            counts[p] += 1
        else:
            counts["UNKNOWN"] += 1
    return counts


def _proto_summary_str(drives):
    c = _proto_counts(drives)
    parts = []
    if c["SAS"] > 0:
        parts.append("SAS:%d" % c["SAS"])
    if c["SATA"] > 0:
        parts.append("SATA:%d" % c["SATA"])
    if c["NVMe"] > 0:
        parts.append("NVMe:%d" % c["NVMe"])
    if c["UNKNOWN"] > 0:
        parts.append("ETC:%d" % c["UNKNOWN"])
    if not parts:
        return "0"
    return ",".join(parts)


def _select_controller(server, sel):
    ctrls = server.get("controllers") or []
    if not ctrls:
        return None
    sel_s = to_text(sel).strip().lower()
    if not sel_s or sel_s == "auto":
        # Prefer first non-boot controller with drives
        for c in ctrls:
            if not c.get("is_boot") and len(c.get("drives") or []) > 0:
                return c
        for c in ctrls:
            if not c.get("is_boot"):
                return c
        return ctrls[0]
    if sel_s.isdigit():
        idx = int(sel_s)
        if 1 <= idx <= len(ctrls):
            return ctrls[idx - 1]
    return None


def _detect_heterogeneity(ok_servers):
    warnings = []
    if len(ok_servers) <= 1:
        return False, warnings

    gens = sorted(list(set([s.get("generation", "Unknown") for s in ok_servers])))
    if len(gens) > 1:
        warnings.append(
            "[경고] 이기종 서버 감지 (세대 혼재): %s - 서버별 컨트롤러/백엔드가 다르므로 디스크 매핑을 반드시 확인하세요."
            % ", ".join(gens)
        )

    models = sorted(list(set([s.get("model", "Unknown") for s in ok_servers])))
    if len(models) > 1 and len(gens) == 1:
        warnings.append("[경고] 서버 모델 혼재: %s" % ", ".join(models))

    ctrl_models = []
    drive_sigs = []
    for s in ok_servers:
        c = _select_controller(s, "auto")
        if c:
            cm = c.get("model") or c.get("name") or "Unknown"
            if cm not in ctrl_models:
                ctrl_models.append(cm)
            ds = "%d개(%s)" % (len(c.get("drives") or []), _proto_summary_str(c.get("drives") or []))
            if ds not in drive_sigs:
                drive_sigs.append(ds)
        else:
            if "NONE" not in ctrl_models:
                ctrl_models.append("NONE")

    if len(ctrl_models) > 1:
        warnings.append("[경고] 스토리지 컨트롤러 모델 상이: %s" % ", ".join(ctrl_models))
    if len(drive_sigs) > 1:
        warnings.append("[경고] 디스크 구성(개수/프로토콜) 상이: %s" % ", ".join(drive_sigs))

    return (len(warnings) > 0), warnings


def _build_controller_menu(ok_servers, heterogeneous):
    menu = [
        {
            "key": "auto",
            "label": "자동 선택 (부팅 컨트롤러 제외 기본 데이터 컨트롤러) [권장]",
            "is_boot": False,
            "backend": "auto",
            "drive_count": 0,
        }
    ]
    if not ok_servers:
        return menu

    if not heterogeneous:
        first_ctrls = ok_servers[0].get("controllers") or []
        for idx, c in enumerate(first_ctrls):
            tag = " [BOOT - 제외 권장]" if c.get("is_boot") else ""
            dcount = len(c.get("drives") or [])
            ps = _proto_summary_str(c.get("drives") or [])
            label = "%s (%s, 디스크 %d개: %s)%s" % (
                c.get("model") or c.get("name"),
                c.get("backend"),
                dcount,
                ps,
                tag,
            )
            menu.append({
                "key": to_text(idx + 1),
                "label": label,
                "is_boot": bool(c.get("is_boot")),
                "backend": c.get("backend", ""),
                "drive_count": dcount,
            })
    else:
        # List unique slot indices / representative controllers across heterogeneous fleet
        max_len = 0
        for s in ok_servers:
            clen = len(s.get("controllers") or [])
            if clen > max_len:
                max_len = clen
        for idx in range(max_len):
            names = []
            any_boot = False
            backends = []
            max_d = 0
            for s in ok_servers:
                ctrls = s.get("controllers") or []
                if idx < len(ctrls):
                    c = ctrls[idx]
                    nm = c.get("model") or c.get("name") or "Unknown"
                    if c.get("is_boot"):
                        any_boot = True
                        nm = nm + " [BOOT - 제외 권장]"
                    if nm not in names:
                        names.append(nm)
                    bk = c.get("backend", "")
                    if bk and bk not in backends:
                        backends.append(bk)
                    dc = len(c.get("drives") or [])
                    if dc > max_d:
                        max_d = dc
            menu.append({
                "key": to_text(idx + 1),
                "label": " / ".join(names),
                "is_boot": any_boot,
                "backend": ",".join(backends),
                "drive_count": max_d,
            })
    return menu


def _idx_to_sd_letters(idx_1based):
    # 1 -> a, 26 -> z, 27 -> aa
    n = idx_1based
    chars = []
    while n > 0:
        n -= 1
        chars.append(chr(ord("a") + (n % 26)))
        n //= 26
    chars.reverse()
    return "".join(chars)


def cmd_summarize(args):
    if len(args) < 1:
        err_print("Usage: summarize <inv_dir> [--format text|json] [--controller-sel auto|N]")
        return 1
    inv_dir = args[0]
    fmt = "text"
    ctrl_sel = None

    i = 1
    while i < len(args):
        a = args[i]
        if a == "--format" and i + 1 < len(args):
            fmt = args[i + 1]
            i += 2
        elif a == "--controller-sel" and i + 1 < len(args):
            ctrl_sel = args[i + 1]
            i += 2
        else:
            i += 1

    invs = _load_inventories(inv_dir)
    ok_servers = [s for s in invs if s.get("status") == "OK"]
    err_servers = [s for s in invs if s.get("status") != "OK"]
    heterogeneous, warnings = _detect_heterogeneity(ok_servers)
    menu = _build_controller_menu(ok_servers, heterogeneous)

    if fmt == "json":
        payload = {
            "ok_count": len(ok_servers),
            "error_count": len(err_servers),
            "heterogeneous": heterogeneous,
            "warnings": warnings,
            "controller_menu": menu,
            "recommended_controller": "auto",
        }
        out_print(to_text(json.dumps(payload, indent=2, ensure_ascii=False)))
        return 0

    if ctrl_sel is None:
        out_print("=== 서버 인벤토리 수집 결과 (정상: %d대, 오류: %d대) ===" % (len(ok_servers), len(err_servers)))
        rows = []
        for s in invs:
            if s.get("status") != "OK":
                rows.append([
                    s.get("ip", ""),
                    "-",
                    "-",
                    "-",
                    "-",
                    "ERROR: %s" % s.get("error", ""),
                ])
                continue
            c_descs = []
            for idx, c in enumerate(s.get("controllers") or []):
                tag = "[BOOT]" if c.get("is_boot") else ""
                c_descs.append("%d:%s%s(%d)" % (idx + 1, c.get("model") or c.get("name"), tag, len(c.get("drives") or [])))
            rows.append([
                s.get("ip", ""),
                s.get("model", ""),
                s.get("generation", ""),
                "%s/%s" % (s.get("power_state", ""), s.get("post_state", "")),
                ", ".join(c_descs) if c_descs else "없음",
                "OK",
            ])
        out_print(format_table(["IP", "Model", "Gen", "Power/POST", "Controllers", "Status"], rows))

        if warnings:
            out_print("")
            for w in warnings:
                out_print(w)

        out_print("")
        out_print("=== 컨트롤러 선택 메뉴 ===")
        for item in menu:
            out_print("  [%s] %s" % (item["key"], item["label"]))
        return 0

    # ctrl_sel IS passed -> show physical drive tables grouped by hardware layout
    groups = []
    group_map = {}
    for s in ok_servers:
        c = _select_controller(s, ctrl_sel)
        if c is None:
            sig = ("NONE", "", ())
            c_info = None
        else:
            d_sigs = []
            for d in c.get("drives") or []:
                d_sigs.append((
                    d.get("location", ""),
                    d.get("protocol", ""),
                    d.get("media_type", ""),
                    d.get("capacity_human", ""),
                    ",".join(d.get("assigned_volumes") or []),
                ))
            sig = (c.get("model", ""), c.get("backend", ""), tuple(d_sigs))
            c_info = c
        if sig not in group_map:
            entry = {"ips": [], "ctrl": c_info}
            group_map[sig] = entry
            groups.append(entry)
        group_map[sig]["ips"].append(s.get("ip", ""))

    out_print("=== 선택된 컨트롤러 (%s) 물리 디스크 목록 ===" % ctrl_sel)
    for gidx, grp in enumerate(groups):
        c = grp["ctrl"]
        out_print("")
        if c is None:
            out_print("[그룹 %d] 서버: %s -> 선택 가능한 컨트롤러 없음" % (gidx + 1, ", ".join(grp["ips"])))
            continue
        boot_tag = " [BOOT - 제외 권장]" if c.get("is_boot") else ""
        out_print("[그룹 %d] 서버(%d대): %s" % (gidx + 1, len(grp["ips"]), ", ".join(grp["ips"])))
        out_print("컨트롤러: %s%s | 백엔드: %s" % (c.get("model") or c.get("name"), boot_tag, c.get("backend")))

        drives = c.get("drives") or []
        sas_sata_n = 0
        nvme_n = 0
        rows = []
        for d in drives:
            alias = ""
            p = d.get("protocol", "UNKNOWN")
            if p in ("SAS", "SATA"):
                sas_sata_n += 1
                alias = "sd%s" % _idx_to_sd_letters(sas_sata_n)
            elif p == "NVMe":
                alias = "nvme%dn1" % nvme_n
                nvme_n += 1
            assigned = ", ".join(d.get("assigned_volumes") or [])
            if not assigned:
                assigned = "-"
            loc_disp = d.get("location", "")
            if alias:
                loc_disp = "%s (%s)" % (loc_disp, alias)
            rows.append([
                to_text(d.get("index", "")),
                loc_disp,
                p,
                d.get("media_type", ""),
                d.get("capacity_human", ""),
                d.get("status", ""),
                assigned,
            ])
        out_print(format_table(["#", "Location", "Protocol", "Media", "Size", "Status", "Assigned Volume"], rows))

        hint_parts = []
        if sas_sata_n > 0:
            hint_parts.append("SAS/SATA %d개 (sda~sd%s)" % (sas_sata_n, _idx_to_sd_letters(sas_sata_n)))
        if nvme_n > 0:
            hint_parts.append("NVMe %d개 (nvme0n1~nvme%dn1)" % (nvme_n, nvme_n - 1))
        if hint_parts:
            out_print("범위 참고: %s | 전체 인덱스: 1-%d" % (", ".join(hint_parts), len(drives)))
    return 0


# ---------------------------------------------------------------------------
# Subcommand 4: plan
# ---------------------------------------------------------------------------

def _sd_letters_to_idx(letters):
    val = 0
    for ch in letters.lower():
        val = val * 26 + (ord(ch) - ord("a") + 1)
    return val


def _parse_disk_range(spec, drives):
    spec_s = to_text(spec).strip()
    if not spec_s:
        return None, "디스크 범위가 비어 있습니다."

    sas_sata = [d for d in drives if d.get("protocol") in ("SAS", "SATA")]
    nvmes = [d for d in drives if d.get("protocol") == "NVMe"]
    loc_map = {}
    for d in drives:
        nl = norm_loc(d.get("location", ""))
        if nl and nl not in loc_map:
            loc_map[nl] = d

    selected = []
    seen_indices = set()

    def add_drive(drv):
        idx = drv.get("index")
        if idx not in seen_indices:
            seen_indices.add(idx)
            selected.append(drv)

    tokens = [t.strip() for t in spec_s.split(",") if t.strip()]
    if not tokens:
        return None, "디스크 범위가 비어 있습니다."

    for tok in tokens:
        # 1. Exact physical location match first
        ntok = norm_loc(tok)
        if ntok in loc_map:
            add_drive(loc_map[ntok])
            continue

        # 2. SAS/SATA Linux range: sda-sdh
        m = re.match(r"^sd([a-z]+)\s*-\s*sd([a-z]+)$", tok, re.I)
        if m:
            s_idx = _sd_letters_to_idx(m.group(1))
            e_idx = _sd_letters_to_idx(m.group(2))
            if s_idx > e_idx:
                return None, "잘못된 SAS/SATA 범위: %s" % tok
            if e_idx > len(sas_sata):
                return None, "SAS/SATA 디스크 수(%d개)를 초과한 범위: %s" % (len(sas_sata), tok)
            for idx in range(s_idx, e_idx + 1):
                add_drive(sas_sata[idx - 1])
            continue

        # 3. Single SAS/SATA: sda
        m = re.match(r"^sd([a-z]+)$", tok, re.I)
        if m:
            idx = _sd_letters_to_idx(m.group(1))
            if idx < 1 or idx > len(sas_sata):
                return None, "SAS/SATA 디스크 범위 초과: %s (보유 %d개)" % (tok, len(sas_sata))
            add_drive(sas_sata[idx - 1])
            continue

        # 4. NVMe range: nvme0n1-nvme7n1 or nvme0-nvme7
        m = re.match(r"^nvme(\d+)(?:n\d+)?\s*-\s*nvme(\d+)(?:n\d+)?$", tok, re.I)
        if m:
            s_num = int(m.group(1))
            e_num = int(m.group(2))
            if s_num > e_num:
                return None, "잘못된 NVMe 범위: %s" % tok
            if e_num >= len(nvmes):
                return None, "NVMe 디스크 수(%d개)를 초과한 범위: %s" % (len(nvmes), tok)
            for num in range(s_num, e_num + 1):
                add_drive(nvmes[num])
            continue

        # 5. Single NVMe: nvme0n1 or nvme0
        m = re.match(r"^nvme(\d+)(?:n\d+)?$", tok, re.I)
        if m:
            num = int(m.group(1))
            if num < 0 or num >= len(nvmes):
                return None, "NVMe 디스크 범위 초과: %s (보유 %d개)" % (tok, len(nvmes))
            add_drive(nvmes[num])
            continue

        # 6. Numeric 1-based range: 1-8
        m = re.match(r"^(\d+)\s*-\s*(\d+)$", tok)
        if m:
            s_idx = int(m.group(1))
            e_idx = int(m.group(2))
            if s_idx < 1 or s_idx > e_idx or e_idx > len(drives):
                return None, "디스크 인덱스 범위 초과: %s (전체 %d개)" % (tok, len(drives))
            for idx in range(s_idx, e_idx + 1):
                add_drive(drives[idx - 1])
            continue

        # 7. Single numeric 1-based index: 1
        m = re.match(r"^(\d+)$", tok)
        if m:
            idx = int(m.group(1))
            if idx < 1 or idx > len(drives):
                return None, "디스크 인덱스 범위 초과: %s (전체 %d개)" % (tok, len(drives))
            add_drive(drives[idx - 1])
            continue

        # 8. Physical location range: 1I:1:1-1I:1:8, P1I:3:1-P1I:3:8, Bay1-Bay8, Bay 1-Bay 8
        m = re.match(r"^(.+?)(\d+)\s*-\s*(.+?)(\d+)$", tok)
        if m and norm_loc(m.group(1)) == norm_loc(m.group(3)):
            prefix_norm = norm_loc(m.group(1))
            s_num = int(m.group(2))
            e_num = int(m.group(4))
            if s_num > e_num:
                return None, "잘못된 물리 위치 범위: %s" % tok
            for num in range(s_num, e_num + 1):
                key = prefix_norm + to_text(num)
                if key not in loc_map:
                    return None, "물리 위치 디스크를 찾을 수 없음: %s%d (범위 %s)" % (m.group(1), num, tok)
                add_drive(loc_map[key])
            continue

        # 9. Shorthand physical location range: 1I:1:1-8
        m = re.match(r"^(.+[:\s])(\d+)\s*-\s*(\d+)$", tok)
        if m:
            prefix_norm = norm_loc(m.group(1))
            s_num = int(m.group(2))
            e_num = int(m.group(3))
            if s_num > e_num:
                return None, "잘못된 물리 위치 범위: %s" % tok
            for num in range(s_num, e_num + 1):
                key = prefix_norm + to_text(num)
                if key not in loc_map:
                    return None, "물리 위치 디스크를 찾을 수 없음: %s%d (범위 %s)" % (m.group(1), num, tok)
                add_drive(loc_map[key])
            continue

        return None, "인식할 수 없는 디스크 범위/위치 토큰: %s" % tok

    if not selected:
        return None, "선택된 디스크가 없습니다."
    return selected, None


def _validate_raid(raid_level, drive_count):
    rl = re.sub(r"^(?:RAID|Raid|raid)", "", to_text(raid_level).strip())
    if rl not in RAID_TO_REDFISH:
        return None, "지원하지 않는 RAID 레벨입니다: %s (지원: 0, 1, 5, 6, 10, 50, 60)" % raid_level
    if rl == "0":
        if drive_count < 1:
            return None, "RAID 0은 최소 1개 이상의 디스크가 필요합니다."
    elif rl == "1":
        if drive_count < 2 or (drive_count % 2) != 0:
            return None, "RAID 1은 2개 이상의 짝수 디스크가 필요합니다 (현재 %d개)." % drive_count
    elif rl == "5":
        if drive_count < 3:
            return None, "RAID 5는 최소 3개 이상의 디스크가 필요합니다 (현재 %d개)." % drive_count
    elif rl == "6":
        if drive_count < 4:
            return None, "RAID 6는 최소 4개 이상의 디스크가 필요합니다 (현재 %d개)." % drive_count
    elif rl == "10":
        if drive_count < 4 or (drive_count % 2) != 0:
            return None, "RAID 10은 4개 이상의 짝수 디스크가 필요합니다 (현재 %d개)." % drive_count
    elif rl == "50":
        if drive_count < 6:
            return None, "RAID 50은 최소 6개 이상의 디스크가 필요합니다 (현재 %d개)." % drive_count
    elif rl == "60":
        if drive_count < 8:
            return None, "RAID 60은 최소 8개 이상의 디스크가 필요합니다 (현재 %d개)." % drive_count
    return rl, None


def _find_conflicts(ctrl, selected_drives):
    sel_uris = set([norm_uri(d.get("odata_id", "")) for d in selected_drives if d.get("odata_id")])
    sel_locs = set([norm_loc(d.get("location", "")) for d in selected_drives if d.get("location")])
    sel_labels = set()
    for d in selected_drives:
        for av in d.get("assigned_volumes") or []:
            sel_labels.add(av)

    all_drives = ctrl.get("drives") or []
    conflicts = []
    seen_vol_keys = set()

    for v in ctrl.get("volumes") or []:
        v_key = norm_uri(v.get("odata_id", "")) or ("id:%s:%s" % (v.get("id", ""), v.get("name", "")))
        v_refs_uri = set([norm_uri(r) for r in v.get("drive_refs", []) if r])
        v_refs_loc = set([norm_loc(r) for r in v.get("drive_refs", []) if r])

        hit = False
        if sel_uris.intersection(v_refs_uri):
            hit = True
        elif sel_locs.intersection(v_refs_loc):
            hit = True
        elif (v.get("name") in sel_labels) or (v.get("id") in sel_labels):
            hit = True

        if not hit or v_key in seen_vol_keys:
            continue
        seen_vol_keys.add(v_key)

        # Check if this volume also spans any unselected drives
        unselected_locs = []
        for d in all_drives:
            du = norm_uri(d.get("odata_id", ""))
            dl = norm_loc(d.get("location", ""))
            in_vol = False
            if du and du in v_refs_uri:
                in_vol = True
            elif dl and dl in v_refs_loc:
                in_vol = True
            elif (v.get("name") in (d.get("assigned_volumes") or [])) or (v.get("id") in (d.get("assigned_volumes") or [])):
                in_vol = True
            if in_vol:
                is_sel = (du in sel_uris) or (dl in sel_locs)
                if not is_sel:
                    unselected_locs.append(d.get("location") or d.get("id"))

        c_entry = {
            "id": v.get("id", ""),
            "name": v.get("name", ""),
            "odata_id": v.get("odata_id", ""),
            "raid_type": v.get("raid_type", ""),
            "drive_refs": list(v.get("drive_refs") or []),
            "spans_unselected": len(unselected_locs) > 0,
            "unselected_drives": unselected_locs,
        }
        if "logical_drive_number" in v:
            c_entry["logical_drive_number"] = v["logical_drive_number"]
        if "unique_identifier" in v:
            c_entry["unique_identifier"] = v["unique_identifier"]
        conflicts.append(c_entry)

    # Edge case: drive had assigned_volumes label but volume object wasn't in ctrl["volumes"]
    if not conflicts and sel_labels:
        for lbl in sorted(list(sel_labels)):
            conflicts.append({
                "id": lbl,
                "name": lbl,
                "odata_id": "",
                "raid_type": "Unknown",
                "drive_refs": [],
                "spans_unselected": False,
                "unselected_drives": [],
            })

    # Sort descending by id/odata_id for safe Redfish deletion order
    conflicts.sort(key=lambda x: natural_sort_key(x.get("id") or x.get("odata_id") or ""), reverse=True)
    return conflicts


def _build_create_volumes(raid_level, raid0_mode, selected_drives, name_prefix, stripe_bytes):
    create_vols = []
    ssc_raid = RAID_TO_SSC[raid_level]
    rf_raid = RAID_TO_REDFISH[raid_level]

    if raid_level == "0" and raid0_mode == "per-disk":
        for idx, d in enumerate(selected_drives):
            vname = ""
            if name_prefix:
                vname = "%s_%02d" % (name_prefix, idx + 1)
            create_vols.append({
                "raid_level": raid_level,
                "ssc_raid": ssc_raid,
                "redfish_raid": rf_raid,
                "name": vname,
                "stripe_bytes": stripe_bytes,
                "drive_locations": [d.get("location", "")],
                "drive_odata_ids": [d.get("odata_id", "")],
            })
    else:
        create_vols.append({
            "raid_level": raid_level,
            "ssc_raid": ssc_raid,
            "redfish_raid": rf_raid,
            "name": name_prefix or "",
            "stripe_bytes": stripe_bytes,
            "drive_locations": [d.get("location", "") for d in selected_drives],
            "drive_odata_ids": [d.get("odata_id", "") for d in selected_drives],
        })
    return create_vols


def _build_preview_payloads(ctrl, delete_volumes, create_volumes):
    previews = []
    backend = ctrl.get("backend", "")
    if backend == "smartstorageconfig":
        del_ids = set([to_text(dv.get("id", "")) for dv in delete_volumes if dv.get("id")])
        del_names = set([to_text(dv.get("name", "")) for dv in delete_volumes if dv.get("name")])
        lds = []
        for v in ctrl.get("volumes") or []:
            if (v.get("id") and to_text(v.get("id")) in del_ids) or (v.get("name") and v.get("name") in del_names):
                continue
            existing_entry = {
                "Raid": v.get("raid_type", ""),
                "DataDrives": list(v.get("drive_refs") or []),
            }
            if v.get("name"):
                existing_entry["LogicalDriveName"] = v["name"]
            if "logical_drive_number" in v:
                existing_entry["LogicalDriveNumber"] = v["logical_drive_number"]
            lds.append(existing_entry)
        for cv in create_volumes:
            new_entry = {
                "Raid": cv["ssc_raid"],
                "DataDrives": list(cv["drive_locations"]),
            }
            if cv.get("name"):
                new_entry["LogicalDriveName"] = cv["name"]
            if cv.get("stripe_bytes") is not None:
                new_entry["StripSizeBytes"] = int(cv["stripe_bytes"])
            lds.append(new_entry)
        previews.append({
            "method": "PUT",
            "path": ctrl.get("settings_uri") or "/redfish/v1/Systems/1/smartstorageconfig/settings/",
            "body": {
                "DataGuard": "Disabled",
                "LogicalDrives": lds,
            },
        })
    else:
        for dv in delete_volumes:
            if dv.get("odata_id"):
                previews.append({
                    "method": "DELETE",
                    "path": dv["odata_id"],
                })
        vol_uri = ctrl.get("volumes_uri", "")
        for cv in create_volumes:
            drive_links = []
            for du in cv.get("drive_odata_ids") or []:
                drive_links.append({"@odata.id": du})
            body = {
                "RAIDType": cv["redfish_raid"],
                "Links": {
                    "Drives": drive_links,
                },
            }
            if cv.get("name"):
                body["DisplayName"] = cv["name"]
            if cv.get("stripe_bytes") is not None:
                body["StripSizeBytes"] = int(cv["stripe_bytes"])
            previews.append({
                "method": "POST",
                "path": vol_uri,
                "body": body,
            })
    return previews


def cmd_plan(args):
    inv_dir = ""
    out_dir = ""
    ctrl_sel = "auto"
    range_spec = ""
    raid_level = ""
    raid0_mode = "combined"
    force_delete = 0
    name_prefix = ""
    stripe_bytes = None

    i = 0
    while i < len(args):
        a = args[i]
        if a == "--inv-dir" and i + 1 < len(args):
            inv_dir = args[i + 1]
            i += 2
        elif a == "--out-dir" and i + 1 < len(args):
            out_dir = args[i + 1]
            i += 2
        elif a == "--controller" and i + 1 < len(args):
            ctrl_sel = args[i + 1]
            i += 2
        elif a == "--range" and i + 1 < len(args):
            range_spec = args[i + 1]
            i += 2
        elif a == "--raid" and i + 1 < len(args):
            raid_level = args[i + 1]
            i += 2
        elif a == "--raid0-mode" and i + 1 < len(args):
            raid0_mode = args[i + 1]
            i += 2
        elif a == "--force-delete" and i + 1 < len(args):
            force_delete = 1 if to_text(args[i + 1]).strip() in ("1", "true", "True", "yes") else 0
            i += 2
        elif a == "--name" and i + 1 < len(args):
            name_prefix = args[i + 1]
            i += 2
        elif a == "--stripe" and i + 1 < len(args):
            raw_st = to_text(args[i + 1]).strip()
            if raw_st:
                if not raw_st.isdigit():
                    err_print("잘못된 --stripe 값 (정수 바이트 필요): %s" % raw_st)
                    return 1
                stripe_bytes = int(raw_st)
            i += 2
        else:
            i += 1

    if not inv_dir or not out_dir:
        err_print("Usage: plan --inv-dir DIR --out-dir DIR --controller SEL --range SPEC --raid LEVEL --raid0-mode combined|per-disk --force-delete 0|1 [--name PREFIX] [--stripe BYTES]")
        return 1

    if raid0_mode not in ("combined", "per-disk"):
        err_print("잘못된 --raid0-mode: %s (combined 또는 per-disk)" % raid0_mode)
        return 1

    if not os.path.isdir(out_dir):
        os.makedirs(out_dir)

    invs = _load_inventories(inv_dir)
    ready_ips = []
    conflict_ips = []
    error_ips = []
    has_deletes = False
    total_delete_count = 0
    plan_briefs = []
    table_rows = []
    span_warnings = []

    for inv in invs:
        ip = inv.get("ip", "")
        sip = safe_ip_name(ip)
        plan_path = os.path.join(out_dir, "plan_%s.json" % sip)

        if inv.get("status") != "OK":
            plan_obj = {
                "ip": ip,
                "status": "ERROR",
                "error": inv.get("error", "Discovery failed"),
                "remediation": "",
                "model": inv.get("model", "Unknown"),
                "generation": inv.get("generation", "Unknown"),
                "backend": "",
                "controller_id": "",
                "controller_name": "",
                "volumes_uri": "",
                "settings_uri": "",
                "raid_level": raid_level,
                "raid0_mode": raid0_mode,
                "range_spec": range_spec,
                "force_delete": bool(force_delete),
                "selected_drives": [],
                "conflicts": [],
                "delete_volumes": [],
                "create_volumes": [],
                "preview_payloads": [],
            }
            dump_json_file(plan_path, plan_obj)
            error_ips.append(ip)
            plan_briefs.append({"ip": ip, "status": "ERROR", "error": plan_obj["error"]})
            table_rows.append([ip, inv.get("model", "-"), inv.get("generation", "-"), "-", "-", "-", "-", "ERROR"])
            continue

        ctrl = _select_controller(inv, ctrl_sel)
        if ctrl is None:
            err_msg = "선택된 컨트롤러(%s)를 찾을 수 없습니다." % ctrl_sel
            plan_obj = {
                "ip": ip,
                "status": "ERROR",
                "error": err_msg,
                "remediation": "",
                "model": inv.get("model", "Unknown"),
                "generation": inv.get("generation", "Unknown"),
                "backend": "",
                "controller_id": "",
                "controller_name": "",
                "volumes_uri": "",
                "settings_uri": "",
                "raid_level": raid_level,
                "raid0_mode": raid0_mode,
                "range_spec": range_spec,
                "force_delete": bool(force_delete),
                "selected_drives": [],
                "conflicts": [],
                "delete_volumes": [],
                "create_volumes": [],
                "preview_payloads": [],
            }
            dump_json_file(plan_path, plan_obj)
            error_ips.append(ip)
            plan_briefs.append({"ip": ip, "status": "ERROR", "error": err_msg})
            table_rows.append([ip, inv.get("model", "-"), inv.get("generation", "-"), "-", "-", "-", "-", "ERROR: %s" % err_msg])
            continue

        selected_drives, range_err = _parse_disk_range(range_spec, ctrl.get("drives") or [])
        if range_err:
            plan_obj = {
                "ip": ip,
                "status": "ERROR",
                "error": range_err,
                "remediation": "",
                "model": inv.get("model", "Unknown"),
                "generation": inv.get("generation", "Unknown"),
                "backend": ctrl.get("backend", ""),
                "controller_id": ctrl.get("id", ""),
                "controller_name": ctrl.get("model") or ctrl.get("name") or "",
                "volumes_uri": ctrl.get("volumes_uri", ""),
                "settings_uri": ctrl.get("settings_uri", ""),
                "raid_level": raid_level,
                "raid0_mode": raid0_mode,
                "range_spec": range_spec,
                "force_delete": bool(force_delete),
                "selected_drives": [],
                "conflicts": [],
                "delete_volumes": [],
                "create_volumes": [],
                "preview_payloads": [],
            }
            dump_json_file(plan_path, plan_obj)
            error_ips.append(ip)
            plan_briefs.append({"ip": ip, "status": "ERROR", "error": range_err})
            table_rows.append([ip, inv.get("model", "-"), inv.get("generation", "-"), ctrl.get("backend", ""), "-", "-", "-", "ERROR: %s" % range_err])
            continue

        norm_raid, raid_err = _validate_raid(raid_level, len(selected_drives))
        if raid_err:
            plan_obj = {
                "ip": ip,
                "status": "ERROR",
                "error": raid_err,
                "remediation": "",
                "model": inv.get("model", "Unknown"),
                "generation": inv.get("generation", "Unknown"),
                "backend": ctrl.get("backend", ""),
                "controller_id": ctrl.get("id", ""),
                "controller_name": ctrl.get("model") or ctrl.get("name") or "",
                "volumes_uri": ctrl.get("volumes_uri", ""),
                "settings_uri": ctrl.get("settings_uri", ""),
                "raid_level": raid_level,
                "raid0_mode": raid0_mode,
                "range_spec": range_spec,
                "force_delete": bool(force_delete),
                "selected_drives": selected_drives,
                "conflicts": [],
                "delete_volumes": [],
                "create_volumes": [],
                "preview_payloads": [],
            }
            dump_json_file(plan_path, plan_obj)
            error_ips.append(ip)
            plan_briefs.append({"ip": ip, "status": "ERROR", "error": raid_err})
            table_rows.append([ip, inv.get("model", "-"), inv.get("generation", "-"), ctrl.get("backend", ""), "%d개" % len(selected_drives), "-", "-", "ERROR: %s" % raid_err])
            continue

        conflicts = _find_conflicts(ctrl, selected_drives)
        create_volumes = _build_create_volumes(norm_raid, raid0_mode, selected_drives, name_prefix, stripe_bytes)

        delete_volumes = []
        remediation = ""
        if conflicts:
            for cf in conflicts:
                if cf.get("spans_unselected"):
                    span_warnings.append(
                        "[주의] %s 볼륨 '%s'은 선택 범위 외 디스크(%s)도 포함하고 있습니다."
                        % (ip, cf.get("name") or cf.get("id"), ", ".join(cf.get("unselected_drives") or []))
                    )
            if force_delete == 1:
                status = "READY_WITH_DELETE"
                delete_volumes = conflicts
                has_deletes = True
                total_delete_count += len(delete_volumes)
                ready_ips.append(ip)
            else:
                status = "CONFLICT"
                remediation = REMEDIATION_FORCE_DELETE
                conflict_ips.append(ip)
        else:
            status = "READY"
            ready_ips.append(ip)

        previews = _build_preview_payloads(ctrl, delete_volumes, create_volumes)

        plan_obj = {
            "ip": ip,
            "status": status,
            "error": "",
            "remediation": remediation,
            "model": inv.get("model", "Unknown"),
            "generation": inv.get("generation", "Unknown"),
            "backend": ctrl.get("backend", ""),
            "controller_id": ctrl.get("id", ""),
            "controller_name": ctrl.get("model") or ctrl.get("name") or "",
            "volumes_uri": ctrl.get("volumes_uri", ""),
            "settings_uri": ctrl.get("settings_uri", ""),
            "raid_level": norm_raid,
            "raid0_mode": raid0_mode,
            "range_spec": range_spec,
            "force_delete": bool(force_delete),
            "selected_drives": selected_drives,
            "conflicts": conflicts,
            "delete_volumes": delete_volumes,
            "create_volumes": create_volumes,
            "preview_payloads": previews,
        }
        dump_json_file(plan_path, plan_obj)

        plan_briefs.append({
            "ip": ip,
            "status": status,
            "backend": ctrl.get("backend", ""),
            "selected_count": len(selected_drives),
            "create_count": len(create_volumes),
            "delete_count": len(delete_volumes),
            "conflict_count": len(conflicts),
        })

        drv_locs = [d.get("location", "") for d in selected_drives]
        if len(drv_locs) <= 4:
            drv_disp = "%d개(%s)" % (len(drv_locs), ",".join(drv_locs))
        else:
            drv_disp = "%d개(%s..%s)" % (len(drv_locs), drv_locs[0], drv_locs[-1])

        raid_disp = "RAID%s" % norm_raid
        if norm_raid == "0":
            raid_disp = "RAID0(%s %d볼륨)" % ("개별" if raid0_mode == "per-disk" else "통합", len(create_volumes))

        if conflicts:
            vol_names = ", ".join([(c.get("name") or c.get("id")) for c in conflicts])
            del_disp = "%d개(%s)" % (len(conflicts), vol_names)
        else:
            del_disp = "없음"

        table_rows.append([
            ip,
            inv.get("model", ""),
            inv.get("generation", ""),
            ctrl.get("backend", ""),
            drv_disp,
            raid_disp,
            del_disp,
            status,
        ])

    summary = {
        "ready_ips": ready_ips,
        "conflict_ips": conflict_ips,
        "error_ips": error_ips,
        "has_deletes": has_deletes,
        "total_delete_count": total_delete_count,
        "remediation_hint": REMEDIATION_FORCE_DELETE if conflict_ips else "",
        "plans": plan_briefs,
    }
    dump_json_file(os.path.join(out_dir, "plan_summary.json"), summary)

    out_print("=== RAID 구성 실행 계획 미리보기 ===")
    out_print(format_table(
        ["IP", "Model", "Gen", "Backend", "Selected Drives", "Target RAID", "Existing/Del Vols", "Status"],
        table_rows
    ))
    if span_warnings:
        out_print("")
        for sw in span_warnings:
            out_print(sw)
    if conflict_ips:
        out_print("")
        out_print("[충돌 감지] 대상 서버: %s" % ", ".join(conflict_ips))
        out_print(REMEDIATION_FORCE_DELETE)
    return 0


# ---------------------------------------------------------------------------
# Subcommand 5: build-ssc-payload
# ---------------------------------------------------------------------------

def _ld_matches_delete(ld, del_vols):
    ld_num = ld.get("LogicalDriveNumber")
    ld_uid = to_text(ld.get("UniqueIdentifier", ""))
    ld_name = to_text(ld.get("LogicalDriveName", ""))
    ld_drives = set([norm_loc(x) for x in (ld.get("DataDrives") or []) if isinstance(x, TEXT_TYPE)])

    for dv in del_vols:
        dv_num = dv.get("logical_drive_number")
        dv_id = to_text(dv.get("id", ""))
        dv_uid = to_text(dv.get("unique_identifier", ""))
        dv_name = to_text(dv.get("name", ""))
        dv_drives = set([norm_loc(x) for x in (dv.get("drive_refs") or []) if isinstance(x, TEXT_TYPE)])

        if ld_num is not None and dv_num is not None and ld_num == dv_num:
            return True
        if ld_num is not None and dv_id and to_text(ld_num) == dv_id:
            return True
        if ld_uid and dv_uid and ld_uid == dv_uid:
            return True
        if ld_name and dv_name and ld_name == dv_name:
            return True
        if ld_drives and dv_drives and ld_drives == dv_drives:
            return True
    return False


def cmd_build_ssc_payload(args):
    if len(args) < 3:
        err_print("Usage: build-ssc-payload <live_settings_json> <plan_json> <out_payload_json>")
        return 1
    live_path = args[0]
    plan_path = args[1]
    out_path = args[2]

    live = load_json_file(live_path)
    if not isinstance(live, dict):
        err_print("유효한 live_settings JSON을 읽을 수 없습니다: %s" % live_path)
        return 1
    plan = load_json_file(plan_path)
    if not isinstance(plan, dict):
        err_print("유효한 plan JSON을 읽을 수 없습니다: %s" % plan_path)
        return 1

    del_vols = plan.get("delete_volumes") or []
    existing_lds = live.get("LogicalDrives")
    if not isinstance(existing_lds, list):
        existing_lds = []

    kept_lds = []
    for ld in existing_lds:
        if not isinstance(ld, dict):
            continue
        if del_vols and _ld_matches_delete(ld, del_vols):
            continue
        kept_lds.append(ld)

    for cv in plan.get("create_volumes") or []:
        rl = to_text(cv.get("raid_level", "0"))
        ssc_raid = cv.get("ssc_raid") or RAID_TO_SSC.get(rl, "Raid0")
        new_ld = {
            "Raid": ssc_raid,
            "DataDrives": list(cv.get("drive_locations") or []),
        }
        if cv.get("name"):
            new_ld["LogicalDriveName"] = cv["name"]
        if cv.get("stripe_bytes") is not None:
            new_ld["StripSizeBytes"] = int(cv["stripe_bytes"])
        kept_lds.append(new_ld)

    settings_uri = (
        plan.get("settings_uri")
        or live.get("@odata.id")
        or "/redfish/v1/Systems/1/smartstorageconfig/settings/"
    )

    payload = {
        "path": to_text(settings_uri),
        "body": {
            "DataGuard": "Disabled",
            "LogicalDrives": kept_lds,
        },
    }
    dump_json_file(out_path, payload)
    return 0


# ---------------------------------------------------------------------------
# Subcommand 6: build-redfish-payloads
# ---------------------------------------------------------------------------

def cmd_build_redfish_payloads(args):
    if len(args) < 2:
        err_print("Usage: build-redfish-payloads <plan_json> <out_dir>")
        return 1
    plan_path = args[0]
    out_dir = args[1]

    plan = load_json_file(plan_path)
    if not isinstance(plan, dict):
        err_print("유효한 plan JSON을 읽을 수 없습니다: %s" % plan_path)
        return 1

    if not os.path.isdir(out_dir):
        os.makedirs(out_dir)

    del_lines = []
    for dv in plan.get("delete_volumes") or []:
        uri = to_text(dv.get("odata_id", "")).strip()
        if uri:
            del_lines.append(uri)
    del_content = ("\n".join(del_lines) + "\n") if del_lines else ""
    write_text_file(os.path.join(out_dir, "deletes.txt"), del_content)

    vol_uri = to_text(plan.get("volumes_uri", "")).strip()
    create_vols = plan.get("create_volumes") or []
    for idx, cv in enumerate(create_vols):
        rl = to_text(cv.get("raid_level", "0"))
        rf_raid = cv.get("redfish_raid") or RAID_TO_REDFISH.get(rl, "RAID0")
        drive_links = []
        for du in cv.get("drive_odata_ids") or []:
            drive_links.append({"@odata.id": to_text(du)})
        body = {
            "RAIDType": rf_raid,
            "Links": {
                "Drives": drive_links,
            },
        }
        if cv.get("name"):
            body["DisplayName"] = cv["name"]
        if cv.get("stripe_bytes") is not None:
            body["StripSizeBytes"] = int(cv["stripe_bytes"])

        payload = {
            "path": vol_uri,
            "body": body,
        }
        out_file = os.path.join(out_dir, "post_%03d.json" % (idx + 1))
        dump_json_file(out_file, payload)
    return 0


# ---------------------------------------------------------------------------
# Subcommand 7: summarize-results
# ---------------------------------------------------------------------------

def cmd_summarize_results(args):
    if len(args) < 1:
        err_print("Usage: summarize-results <result_dir> [--format text|json] [--reboot-list-out FILE] [--print-reboot-list]")
        return 1
    result_dir = args[0]
    fmt = "text"
    reboot_list_out = None
    print_reboot_list = False

    i = 1
    while i < len(args):
        a = args[i]
        if a == "--format" and i + 1 < len(args):
            fmt = args[i + 1]
            i += 2
        elif a == "--reboot-list-out" and i + 1 < len(args):
            reboot_list_out = args[i + 1]
            i += 2
        elif a == "--print-reboot-list":
            print_reboot_list = True
            i += 1
        else:
            i += 1

    results = []
    if os.path.isdir(result_dir):
        fnames = [fn for fn in os.listdir(result_dir) if fn.startswith("res_") and fn.endswith(".json")]
        if not fnames:
            fnames = [fn for fn in os.listdir(result_dir) if fn.endswith(".json")]
        for fn in fnames:
            data = load_json_file(os.path.join(result_dir, fn))
            if isinstance(data, dict) and data.get("ip"):
                results.append(data)
    results.sort(key=lambda x: ip_sort_key(x.get("ip", "")))

    reboot_entries = [r for r in results if bool(r.get("reboot_required"))]
    reboot_ips = [r.get("ip", "") for r in reboot_entries]

    if reboot_list_out is not None:
        content = ("\n".join(reboot_ips) + "\n") if reboot_ips else ""
        write_text_file(reboot_list_out, content)

    counts = {"applied": 0, "reboot_required": 0, "failed": 0, "skipped": 0}
    for r in results:
        st = to_text(r.get("status", "")).upper()
        if st == "REBOOT_REQUIRED":
            counts["reboot_required"] += 1
        elif st in ("APPLIED", "OK", "REBOOTED"):
            counts["applied"] += 1
        elif st == "SKIPPED":
            counts["skipped"] += 1
        else:
            counts["failed"] += 1

    if fmt == "json":
        out_print(to_text(json.dumps({
            "results": results,
            "reboot_ips": reboot_ips,
            "counts": counts,
        }, indent=2, ensure_ascii=False)))
        return 0

    rows = []
    for r in results:
        rows.append([
            r.get("ip", ""),
            r.get("model", ""),
            r.get("generation", ""),
            r.get("backend", ""),
            r.get("status", ""),
            r.get("message", ""),
        ])
    out_print("=== 작업 결과 요약 ===")
    out_print(format_table(["IP", "Model", "Gen", "Backend", "Status", "Message"], rows))
    out_print(
        "합계: 적용완료 %d | 재부팅필요 %d | 실패 %d | 건너뜀 %d"
        % (counts["applied"], counts["reboot_required"], counts["failed"], counts["skipped"])
    )

    if print_reboot_list and reboot_entries:
        out_print("")
        out_print("=== 재부팅이 필요한 서버 목록 (%d대) ===" % len(reboot_entries))
        reb_rows = []
        for r in reboot_entries:
            reb_rows.append([
                r.get("ip", ""),
                r.get("model", ""),
                r.get("generation", ""),
                r.get("message", "") or "SmartStorageConfig/RAID 변경 적용을 위해 재부팅 필요",
            ])
        out_print(format_table(["IP", "Model", "Gen", "Reason"], reb_rows))

    return 0


# ---------------------------------------------------------------------------
# Subcommand 8: write-result
# ---------------------------------------------------------------------------

def cmd_write_result(args):
    if len(args) < 8:
        err_print("Usage: write-result <out_json> <ip> <model> <generation> <backend> <status> <reboot_required_0_or_1> <message>")
        return 1
    out_json = args[0]
    ip = to_text(args[1]).strip()
    model = to_text(args[2]).strip()
    generation = to_text(args[3]).strip()
    backend = to_text(args[4]).strip()
    status = to_text(args[5]).strip()
    reb_raw = to_text(args[6]).strip().lower()
    reboot_required = reb_raw in ("1", "true", "yes")
    message = " ".join([to_text(x) for x in args[7:]])

    obj = {
        "ip": ip,
        "model": model,
        "generation": generation,
        "backend": backend,
        "status": status,
        "reboot_required": reboot_required,
        "message": message,
    }
    dump_json_file(out_json, obj)
    return 0


# ---------------------------------------------------------------------------
# Helper subcommand: get-field (read scalar/list field cleanly from JSON)
# ---------------------------------------------------------------------------

def cmd_get_field(args):
    if len(args) < 2:
        err_print("Usage: get-field <json_file> <field_path>")
        return 1
    data = load_json_file(args[0])
    if data is None:
        return 0
    val = get_nested(data, args[1].split("."))
    if val is None:
        return 0
    if isinstance(val, bool):
        out_print("1" if val else "0")
    elif isinstance(val, list):
        for item in val:
            if isinstance(item, (dict, list)):
                out_print(to_text(json.dumps(item, ensure_ascii=False)))
            else:
                out_print(to_text(item))
    elif isinstance(val, dict):
        out_print(to_text(json.dumps(val, indent=2, ensure_ascii=False)))
    else:
        out_print(to_text(val))
    return 0


def main():
    raw_args = [to_text(a) for a in sys.argv[1:]]
    if not raw_args:
        err_print("Usage: raid_parser.py <subcommand> [args...]")
        return 1

    subcmd = raw_args[0]
    rest = raw_args[1:]

    dispatch = {
        "extract-links": cmd_extract_links,
        "normalize": cmd_normalize,
        "summarize": cmd_summarize,
        "plan": cmd_plan,
        "build-ssc-payload": cmd_build_ssc_payload,
        "build-redfish-payloads": cmd_build_redfish_payloads,
        "summarize-results": cmd_summarize_results,
        "write-result": cmd_write_result,
        "get-field": cmd_get_field,
    }
    fn = dispatch.get(subcmd)
    if fn is None:
        err_print("Unknown subcommand: %s" % subcmd)
        return 1
    return fn(rest)


if __name__ == "__main__":
    sys.exit(main())
