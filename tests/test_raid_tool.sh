#!/usr/bin/env bash
# Self-contained integration & static compatibility test suite for HPE iLO RAID tool.
# Compatible with Bash 4.1 (RHEL 6) and Git Bash on Windows.

set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ILO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Detect Python binary
detect_python() {
    if [ -n "${PYTHON_BIN:-}" ]; then
        if "$PYTHON_BIN" -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
            printf '%s' "$PYTHON_BIN"
            return 0
        fi
    fi
    if [ -x "/c/Users/qazx2/AppData/Local/Programs/Python/Python311/python.exe" ]; then
        if "/c/Users/qazx2/AppData/Local/Programs/Python/Python311/python.exe" -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
            printf '%s' "/c/Users/qazx2/AppData/Local/Programs/Python/Python311/python.exe"
            return 0
        fi
    fi
    if command -v python3 >/dev/null 2>&1; then
        if python3 -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
            printf '%s' "python3"
            return 0
        fi
    fi
    if command -v python >/dev/null 2>&1; then
        if python -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
            printf '%s' "python"
            return 0
        fi
    fi
    return 1
}

to_py_path() {
    if command -v cygpath >/dev/null 2>&1; then
        cygpath -m "$1"
    else
        printf '%s' "$1"
    fi
}

PYTHON_BIN="$(detect_python)"
if [ -z "$PYTHON_BIN" ]; then
    echo "FATAL: No working Python interpreter found." >&2
    exit 1
fi
export PYTHON_BIN

PASS_COUNT=0
FAIL_COUNT=0
TEST_SECRET="SuperSecretPass!99"

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ilo_raid_test.XXXXXX")"
MOCK_BIN="$TEST_ROOT/mock_bin"
MOCK_LIB="$TEST_ROOT/mock_lib"
WORK_ROOT="$TEST_ROOT/work"
mkdir -p "$MOCK_BIN" "$MOCK_LIB" "$WORK_ROOT"

cleanup() {
    rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    printf '[PASS] %s\n' "$1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf '[FAIL] %s\n' "$1" >&2
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local label="$3"
    case "$haystack" in
        *"$needle"*)
            pass "$label"
            ;;
        *)
            fail "$label (expected substring: $needle)"
            ;;
    esac
}

assert_not_contains() {
    local haystack="$1"
    local needle="$2"
    local label="$3"
    case "$haystack" in
        *"$needle"*)
            fail "$label (unexpected substring found: $needle)"
            ;;
        *)
            pass "$label"
            ;;
    esac
}

assert_eq() {
    local actual="$1"
    local expected="$2"
    local label="$3"
    if [ "$actual" = "$expected" ]; then
        pass "$label"
    else
        fail "$label (expected='$expected', actual='$actual')"
    fi
}

# ---------------------------------------------------------------------------
# Generate Mock Server Fixture Engine (Python)
# ---------------------------------------------------------------------------
cat > "$MOCK_LIB/mock_redfish.py" <<'PYEOF'
#!/usr/bin/env python
from __future__ import print_function
import sys
import os
import json


def build_gen10_fixtures():
    f = {}
    f["/redfish/v1/Systems/1"] = {
        "@odata.id": "/redfish/v1/Systems/1",
        "Id": "1",
        "Model": "ProLiant DL380 Gen10",
        "PowerState": "On",
        "Oem": {
            "Hpe": {
                "PostState": "FinishedPost",
                "Links": {
                    "SmartStorageConfig": {
                        "@odata.id": "/redfish/v1/Systems/1/smartstorageconfig/"
                    }
                }
            }
        }
    }
    f["/redfish/v1/Managers/1"] = {
        "@odata.id": "/redfish/v1/Managers/1",
        "Id": "1",
        "FirmwareVersion": "iLO 5 v2.95"
    }
    # Read-only /Storage stub mirroring P408i-a to test Gen10 deduplication
    f["/redfish/v1/Systems/1/Storage"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000"}
        ]
    }
    f["/redfish/v1/Systems/1/Storage/DA000000"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DA000000",
        "Id": "DA000000",
        "Name": "HPE Smart Array P408i-a SR Gen10",
        "StorageControllers": [
            {
                "Model": "HPE Smart Array P408i-a SR Gen10",
                "SerialNumber": "PEYHD0ARH12345",
                "Location": {"PartLocation": {"ServiceLabel": "Slot 0"}}
            }
        ],
        "Drives": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/0"}
        ]
    }
    f["/redfish/v1/Systems/1/Storage/DA000000/Drives/0"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/0",
        "Id": "0",
        "Name": "HPE SAS Drive 1I:1:1",
        "Protocol": "SAS",
        "MediaType": "HDD",
        "CapacityBytes": 1200243695616,
        "PhysicalLocation": {"PartLocation": {"ServiceLabel": "1I:1:1"}},
        "Status": {"State": "Enabled", "Health": "OK"}
    }

    # SmartStorage/ArrayControllers
    f["/redfish/v1/Systems/1/SmartStorage/ArrayControllers"] = {
        "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0"}
        ]
    }
    f["/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0"] = {
        "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0",
        "Id": "0",
        "Model": "HPE Smart Array P408i-a SR Gen10",
        "Name": "HPE Smart Array P408i-a SR Gen10",
        "SerialNumber": "PEYHD0ARH12345",
        "Location": "Slot 0",
        "Links": {
            "PhysicalDrives": {
                "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives/"
            },
            "LogicalDrives": {
                "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives/"
            }
        }
    }

    dd_members = []
    for idx in range(1, 11):
        uri = "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives/%d" % idx
        dd_members.append({"@odata.id": uri})
        is_os = (idx >= 9)
        f[uri] = {
            "@odata.id": uri,
            "Id": str(idx),
            "Location": "1I:1:%d" % idx,
            "InterfaceType": "SAS",
            "MediaType": "SSD" if is_os else "HDD",
            "CapacityGB": 480 if is_os else 1200,
            "CapacityMiB": 457862 if is_os else 1144734,
            "Status": {"State": "Enabled", "Health": "OK"}
        }
    f["/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives"] = {
        "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/DiskDrives/",
        "Members": dd_members
    }

    f["/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives"] = {
        "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives/",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives/1"}
        ]
    }
    f["/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives/1"] = {
        "@odata.id": "/redfish/v1/Systems/1/SmartStorage/ArrayControllers/0/LogicalDrives/1",
        "Id": "1",
        "LogicalDriveName": "OS_RAID1",
        "Raid": "Raid1",
        "CapacityMiB": 457862,
        "DataDrives": ["1I:1:9", "1I:1:10"]
    }

    f["/redfish/v1/Systems/1/smartstorageconfig"] = {
        "@odata.id": "/redfish/v1/Systems/1/smartstorageconfig/",
        "Location": "Slot 0",
        "SerialNumber": "PEYHD0ARH12345",
        "@Redfish.Settings": {
            "SettingsObject": {
                "@odata.id": "/redfish/v1/Systems/1/smartstorageconfig/settings/"
            }
        },
        "LogicalDrives": [
            {
                "LogicalDriveName": "OS_RAID1",
                "LogicalDriveNumber": 1,
                "Raid": "Raid1",
                "DataDrives": ["1I:1:9", "1I:1:10"],
                "CapacityGiB": 447
            }
        ]
    }
    f["/redfish/v1/Systems/1/smartstorageconfig/settings"] = {
        "@odata.id": "/redfish/v1/Systems/1/smartstorageconfig/settings/",
        "DataGuard": "Permissive",
        "LogicalDrives": [
            {
                "LogicalDriveName": "OS_RAID1",
                "LogicalDriveNumber": 1,
                "Raid": "Raid1",
                "DataDrives": ["1I:1:9", "1I:1:10"],
                "CapacityGiB": 447
            }
        ]
    }
    return f


def build_gen10plus_fixtures():
    f = {}
    f["/redfish/v1/Systems/1"] = {
        "@odata.id": "/redfish/v1/Systems/1",
        "Id": "1",
        "Model": "ProLiant DL380 Gen10 Plus",
        "PowerState": "On",
        "Oem": {"Hpe": {"PostState": "FinishedPost"}}
    }
    f["/redfish/v1/Managers/1"] = {
        "@odata.id": "/redfish/v1/Managers/1",
        "Id": "1",
        "FirmwareVersion": "iLO 5 v3.05"
    }
    f["/redfish/v1/Systems/1/Storage"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000"},
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000"}
        ]
    }

    # Boot controller NS204i-p
    f["/redfish/v1/Systems/1/Storage/DA000000"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DA000000",
        "Id": "DA000000",
        "Name": "HPE NS204i-p Gen10 Plus Boot Controller",
        "StorageControllers": [
            {"Model": "HPE NS204i-p Gen10 Plus Boot Controller", "SerialNumber": "NSBOOT0001"}
        ],
        "Drives": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/0"},
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/1"}
        ],
        "Volumes": {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Volumes"}
    }
    for bidx in range(2):
        uri = "/redfish/v1/Systems/1/Storage/DA000000/Drives/%d" % bidx
        f[uri] = {
            "@odata.id": uri,
            "Id": str(bidx),
            "Name": "Boot NVMe %d" % (bidx + 1),
            "Protocol": "NVMe",
            "MediaType": "SSD",
            "CapacityBytes": 480103981056,
            "PhysicalLocation": {"PartLocation": {"ServiceLabel": "M.2 Bay %d" % (bidx + 1)}},
            "Status": {"State": "Enabled", "Health": "OK"},
            "Links": {
                "Volumes": [
                    {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Volumes/1"}
                ]
            }
        }
    f["/redfish/v1/Systems/1/Storage/DA000000/Volumes"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Volumes",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Volumes/1"}
        ]
    }
    f["/redfish/v1/Systems/1/Storage/DA000000/Volumes/1"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Volumes/1",
        "Id": "1",
        "Name": "OS_BOOT_RAID1",
        "RAIDType": "RAID1",
        "CapacityBytes": 480103981056,
        "Links": {
            "Drives": [
                {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/0"},
                {"@odata.id": "/redfish/v1/Systems/1/Storage/DA000000/Drives/1"}
            ]
        }
    }

    # Main controller MR416i-a
    mr_drives = []
    for idx in range(1, 9):
        uri = "/redfish/v1/Systems/1/Storage/DE040000/Drives/%d" % idx
        mr_drives.append({"@odata.id": uri})
        links = {}
        if idx in (1, 2):
            links = {
                "Volumes": [
                    {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes/1"}
                ]
            }
        f[uri] = {
            "@odata.id": uri,
            "Id": str(idx),
            "Name": "NVMe SSD Bay %d" % idx,
            "Protocol": "NVMe",
            "MediaType": "SSD",
            "CapacityBytes": 1920383410176,
            "PhysicalLocation": {"PartLocation": {"ServiceLabel": "Bay %d" % idx}},
            "Status": {"State": "Enabled", "Health": "OK"},
            "Links": links
        }
    f["/redfish/v1/Systems/1/Storage/DE040000"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE040000",
        "Id": "DE040000",
        "Name": "HPE MR416i-a Gen10 Plus",
        "StorageControllers": [
            {"Model": "HPE MR416i-a Gen10 Plus", "SerialNumber": "MR416SER0002"}
        ],
        "Drives": mr_drives,
        "Volumes": {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes"}
    }
    f["/redfish/v1/Systems/1/Storage/DE040000/Volumes"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes/1"}
        ]
    }
    f["/redfish/v1/Systems/1/Storage/DE040000/Volumes/1"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes/1",
        "Id": "1",
        "Name": "Existing_Data_RAID1",
        "RAIDType": "RAID1",
        "CapacityBytes": 1920383410176,
        "Links": {
            "Drives": [
                {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Drives/1"},
                {"@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Drives/2"}
            ]
        }
    }
    f["/redfish/v1/Systems/1/Storage/DE040000/Volumes/Capabilities"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE040000/Volumes/Capabilities",
        "RAIDType@Redfish.AllowableValues": ["RAID0", "RAID1", "RAID5", "RAID6", "RAID10", "RAID50", "RAID60"]
    }
    return f


def build_gen11_fixtures():
    f = {}
    f["/redfish/v1/Systems/1"] = {
        "@odata.id": "/redfish/v1/Systems/1",
        "Id": "1",
        "Model": "ProLiant DL380 Gen11",
        "PowerState": "On",
        "Oem": {"Hpe": {"PostState": "FinishedPost"}}
    }
    f["/redfish/v1/Managers/1"] = {
        "@odata.id": "/redfish/v1/Managers/1",
        "Id": "1",
        "FirmwareVersion": "iLO 6 v1.58"
    }
    f["/redfish/v1/Systems/1/Storage"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DE041000"}
        ]
    }

    drives_list = []
    # 8 SAS HDDs: 1I:1:1 .. 1I:1:8
    for idx in range(1, 9):
        uri = "/redfish/v1/Systems/1/Storage/DE041000/Drives/sas%d" % idx
        drives_list.append({"@odata.id": uri})
        f[uri] = {
            "@odata.id": uri,
            "Id": "sas%d" % idx,
            "Name": "SAS HDD 1I:1:%d" % idx,
            "Protocol": "SAS",
            "MediaType": "HDD",
            "CapacityBytes": 2400476553216,
            "PhysicalLocation": {"PartLocation": {"ServiceLabel": "1I:1:%d" % idx}},
            "Status": {"State": "Enabled", "Health": "OK"}
        }
    # 8 NVMe SSDs: 2I:1:1 .. 2I:1:8
    for idx in range(1, 9):
        uri = "/redfish/v1/Systems/1/Storage/DE041000/Drives/nvme%d" % idx
        drives_list.append({"@odata.id": uri})
        f[uri] = {
            "@odata.id": uri,
            "Id": "nvme%d" % idx,
            "Name": "NVMe SSD 2I:1:%d" % idx,
            "Protocol": "NVMe",
            "MediaType": "SSD",
            "CapacityBytes": 1920383410176,
            "PhysicalLocation": {"PartLocation": {"ServiceLabel": "2I:1:%d" % idx}},
            "Status": {"State": "Enabled", "Health": "OK"}
        }

    f["/redfish/v1/Systems/1/Storage/DE041000"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE041000",
        "Id": "DE041000",
        "Name": "HPE SR932i-p Gen11",
        "StorageControllers": [
            {"Model": "HPE SR932i-p Gen11", "SerialNumber": "SR932SER0003"}
        ],
        "Drives": drives_list,
        "Volumes": {"@odata.id": "/redfish/v1/Systems/1/Storage/DE041000/Volumes"}
    }
    f["/redfish/v1/Systems/1/Storage/DE041000/Volumes"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE041000/Volumes",
        "Members": []
    }
    f["/redfish/v1/Systems/1/Storage/DE041000/Volumes/Capabilities"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE041000/Volumes/Capabilities",
        "RAIDType@Redfish.AllowableValues": ["RAID0", "RAID1", "RAID5", "RAID6", "RAID10", "RAID50", "RAID60"]
    }
    return f


def build_gen11_hba_raw_fixtures():
    """Gen11 SR932i-p (iLO 6) with HBA/Raw drives auto-exposed as RAIDType='None' volumes."""
    f = {}
    f["/redfish/v1/Systems/1"] = {
        "@odata.id": "/redfish/v1/Systems/1",
        "Id": "1",
        "Model": "ProLiant DL380 Gen11",
        "PowerState": "On",
        "Oem": {"Hpe": {"PostState": "FinishedPost"}}
    }
    f["/redfish/v1/Managers/1"] = {
        "@odata.id": "/redfish/v1/Managers/1",
        "Id": "1",
        "FirmwareVersion": "iLO 6 v1.78"
    }
    f["/redfish/v1/Systems/1/Storage"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage",
        "Members": [
            {"@odata.id": "/redfish/v1/Systems/1/Storage/DE009000"}
        ]
    }
    drives_list = []
    vol_members = []
    for idx in range(1, 9):
        vid = 53 + idx
        d_uri = "/redfish/v1/Systems/1/Storage/DE009000/Drives/%d" % idx
        v_uri = "/redfish/v1/Systems/1/Storage/DE009000/Volumes/%d" % vid
        drives_list.append({"@odata.id": d_uri})
        vol_members.append({"@odata.id": v_uri})
        f[d_uri] = {
            "@odata.id": d_uri,
            "Id": str(idx),
            "Name": "SAS HDD 1I:1:%d" % idx,
            "Protocol": "SAS",
            "MediaType": "HDD",
            "CapacityBytes": 2400476553216,
            "PhysicalLocation": {"PartLocation": {"ServiceLabel": "1I:1:%d" % idx}},
            "Status": {"State": "Enabled", "Health": "OK"},
            "Links": {"Volumes": [{"@odata.id": v_uri}]}
        }
        f[v_uri] = {
            "@odata.id": v_uri,
            "Id": str(vid),
            "Name": "HBA_Drive_%d" % vid,
            "RAIDType": "None",
            "CapacityBytes": 2400476553216,
            "Links": {"Drives": [{"@odata.id": d_uri}]}
        }
    f["/redfish/v1/Systems/1/Storage/DE009000"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE009000",
        "Id": "DE009000",
        "Name": "HPE SR932i-p Gen11",
        "StorageControllers": [
            {"Model": "HPE SR932i-p Gen11", "SerialNumber": "SR932HBA0004"}
        ],
        "Drives": drives_list,
        "Volumes": {"@odata.id": "/redfish/v1/Systems/1/Storage/DE009000/Volumes"}
    }
    f["/redfish/v1/Systems/1/Storage/DE009000/Volumes"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE009000/Volumes",
        "Members": vol_members
    }
    f["/redfish/v1/Systems/1/Storage/DE009000/Volumes/Capabilities"] = {
        "@odata.id": "/redfish/v1/Systems/1/Storage/DE009000/Volumes/Capabilities",
        "RAIDType@Redfish.AllowableValues": ["RAID0", "RAID1", "RAID5", "RAID6", "RAID10"]
    }
    return f


SERVERS = {
    "10.10.0.1": build_gen10_fixtures,
    "10.10.0.2": build_gen10plus_fixtures,
    "10.10.0.3": build_gen11_fixtures,
    "10.10.0.4": build_gen11_hba_raw_fixtures,
}


def main():
    if len(sys.argv) < 3:
        sys.stderr.write("Usage: mock_redfish.py <ip> <uri>\n")
        sys.exit(2)
    ip = sys.argv[1]
    uri = sys.argv[2].rstrip("/")
    if not uri:
        uri = "/"
    builder = SERVERS.get(ip)
    if builder is None:
        sys.stderr.write("Mock server not found: %s\n" % ip)
        sys.exit(1)
    fixtures = builder()
    data = fixtures.get(uri)
    if data is None:
        sys.stderr.write("404 Not Found: %s on %s\n" % (uri, ip))
        sys.exit(1)
    print(json.dumps(data, indent=2))


if __name__ == "__main__":
    main()
PYEOF

# ---------------------------------------------------------------------------
# Generate Mock pdsh executable
# ---------------------------------------------------------------------------
cat > "$MOCK_BIN/pdsh" <<'EOF'
#!/usr/bin/env bash
set -u

SHOW_MODULES=0
RCMD=""
FANOUT=10
WCOLL=""

while [ $# -gt 0 ]; do
    case "$1" in
        -L)
            SHOW_MODULES=1
            shift
            ;;
        -R)
            RCMD="${2:-}"
            shift 2
            ;;
        -R*)
            RCMD="${1#-R}"
            shift
            ;;
        -f)
            FANOUT="${2:-10}"
            shift 2
            ;;
        -f*)
            FANOUT="${1#-f}"
            shift
            ;;
        -w)
            WCOLL="${2:-}"
            shift 2
            ;;
        -w*)
            WCOLL="${1#-w}"
            shift
            ;;
        --)
            shift
            break
            ;;
        -*)
            shift
            ;;
        *)
            break
            ;;
    esac
done

if [ "$SHOW_MODULES" -eq 1 ]; then
    cat <<'MODS'
3 modules loaded:
  Module: rcmd/exec
  Author: Mock PDSH
  Description: arbitrary command rcmd connect method
  Active: yes
MODS
    exit 0
fi

if [ "$RCMD" != "exec" ]; then
    echo "mock pdsh: expected -R exec, got '$RCMD'" >&2
    exit 2
fi

if [ -z "$WCOLL" ]; then
    echo "mock pdsh: missing -w <hosts>" >&2
    exit 2
fi

HOSTS=""
case "$WCOLL" in
    ^*)
        HOST_FILE="${WCOLL#^}"
        if [ ! -f "$HOST_FILE" ]; then
            echo "mock pdsh: host file not found: $HOST_FILE" >&2
            exit 2
        fi
        while IFS= read -r line || [ -n "$line" ]; do
            line="${line%$'\r'}"
            case "$line" in
                ""|\#*) continue ;;
            esac
            HOSTS="${HOSTS}${line}
"
        done < "$HOST_FILE"
        ;;
    *)
        HOSTS="$(printf '%s\n' "$WCOLL" | tr ',' '\n')"
        ;;
esac

if [ $# -eq 0 ]; then
    echo "mock pdsh: missing command" >&2
    exit 2
fi

CMD_TEMPLATE=("$@")

PIDS=()
HOST_LIST=()
while IFS= read -r host; do
    [ -z "$host" ] && continue
    HOST_LIST+=("$host")
done <<EOF_H
$HOSTS
EOF_H

OVERALL_RC=0
BATCH_PIDS=()
BATCH_COUNT=0

for host in "${HOST_LIST[@]}"; do
    (
        ARGS=()
        for tok in "${CMD_TEMPLATE[@]}"; do
            ARGS+=("${tok//%h/$host}")
        done
        case "${ARGS[0]}" in
            *.sh)
                echo "mock pdsh: ${ARGS[0]} failed: Permission denied (must invoke via bash)" >&2
                exit 126
                ;;
        esac
        "${ARGS[@]}" 2>&1 | while IFS= read -r out_line; do
            printf '%s: %s\n' "$host" "$out_line"
        done
        exit "${PIPESTATUS[0]}"
    ) &
    BATCH_PIDS+=("$!")
    BATCH_COUNT=$((BATCH_COUNT + 1))
    if [ "$BATCH_COUNT" -ge "$FANOUT" ]; then
        for pid in "${BATCH_PIDS[@]}"; do
            wait "$pid" || OVERALL_RC=1
        done
        BATCH_PIDS=()
        BATCH_COUNT=0
    fi
done

for pid in "${BATCH_PIDS[@]}"; do
    wait "$pid" || OVERALL_RC=1
done

exit "$OVERALL_RC"
EOF
chmod +x "$MOCK_BIN/pdsh"

# ---------------------------------------------------------------------------
# Generate Mock ilorest executable
# ---------------------------------------------------------------------------
cat > "$MOCK_BIN/ilorest" <<'EOF'
#!/usr/bin/env bash
set -u

AUDIT_DIR="${MOCK_AUDIT_DIR:-}"
MOCK_LIB_DIR="${MOCK_LIB_DIR:-}"
PY="${PYTHON_BIN:-python3}"

to_py() {
    if command -v cygpath >/dev/null 2>&1; then
        cygpath -m "$1"
    else
        printf '%s' "$1"
    fi
}

CACHE_DIR=""
SUBCMD=""

# Parse global options BEFORE subcommand
while [ $# -gt 0 ]; do
    case "$1" in
        --cache-dir)
            CACHE_DIR="${2:-}"
            shift 2
            ;;
        --cache-dir=*)
            CACHE_DIR="${1#--cache-dir=}"
            shift
            ;;
        --nologo|--silent|-v|--version)
            shift
            ;;
        -*)
            shift
            ;;
        *)
            SUBCMD="$1"
            shift
            break
            ;;
    esac
done

# Strict validation: --cache-dir MUST precede subcommand
if [ -z "$CACHE_DIR" ]; then
    echo "MOCK ILOREST ERROR: --cache-dir MUST be passed BEFORE subcommand '$SUBCMD'" >&2
    if [ -n "$AUDIT_DIR" ]; then
        printf 'ERROR|missing_cache_dir|%s\n' "$SUBCMD" >> "$AUDIT_DIR/errors.log"
    fi
    exit 2
fi

# Check that --cache-dir is not passed AFTER subcommand
for arg in "$@"; do
    case "$arg" in
        --cache-dir|--cache-dir=*)
            echo "MOCK ILOREST ERROR: --cache-dir passed AFTER subcommand '$SUBCMD'" >&2
            if [ -n "$AUDIT_DIR" ]; then
                printf 'ERROR|late_cache_dir|%s\n' "$SUBCMD" >> "$AUDIT_DIR/errors.log"
            fi
            exit 2
            ;;
    esac
done

mkdir -p "$CACHE_DIR"
SESSION_FILE="$CACHE_DIR/.mock_session_ip"

log_call() {
    if [ -n "$AUDIT_DIR" ]; then
        printf '%s\n' "$1" >> "$AUDIT_DIR/calls.log"
    fi
}

case "$SUBCMD" in
    login)
        URL=""
        USER_VAL=""
        PASS_VAL=""
        while [ $# -gt 0 ]; do
            case "$1" in
                -u|--user)
                    USER_VAL="${2:-}"
                    shift 2
                    ;;
                -p|--password)
                    PASS_VAL="${2:-}"
                    shift 2
                    ;;
                -*)
                    shift
                    ;;
                *)
                    if [ -z "$URL" ]; then
                        URL="$1"
                    fi
                    shift
                    ;;
            esac
        done
        if [ -z "$URL" ] || [ -z "$USER_VAL" ] || [ -z "$PASS_VAL" ]; then
            echo "MOCK ILOREST ERROR: login requires <url> -u <user> -p <pass>" >&2
            exit 1
        fi
        case "$URL" in
            https://*)
                IP="${URL#https://}"
                IP="${IP%%/*}"
                ;;
            *)
                echo "MOCK ILOREST ERROR: login URL must start with https:// (got '$URL')" >&2
                exit 1
                ;;
        esac
        if [ -f "$SESSION_FILE" ]; then
            EXISTING_IP="$(cat "$SESSION_FILE" 2>/dev/null || true)"
            if [ -n "$EXISTING_IP" ] && [ "$EXISTING_IP" != "$IP" ]; then
                echo "MOCK ILOREST ERROR: cache-dir '$CACHE_DIR' shared between '$EXISTING_IP' and '$IP'!" >&2
                if [ -n "$AUDIT_DIR" ]; then
                    printf 'ERROR|shared_cache_dir|%s|%s|%s\n' "$EXISTING_IP" "$IP" "$CACHE_DIR" >> "$AUDIT_DIR/errors.log"
                fi
                exit 3
            fi
        fi
        printf '%s' "$IP" > "$SESSION_FILE"
        log_call "login|${IP}|${CACHE_DIR}"
        echo "Discovering data...Done"
        exit 0
        ;;
    logout)
        IP="unknown"
        if [ -f "$SESSION_FILE" ]; then
            IP="$(cat "$SESSION_FILE" 2>/dev/null || printf 'unknown')"
            rm -f "$SESSION_FILE"
        fi
        log_call "logout|${IP}|${CACHE_DIR}"
        echo "Logging session out."
        exit 0
        ;;
    rawget)
        if [ ! -f "$SESSION_FILE" ]; then
            echo "MOCK ILOREST ERROR: rawget called without active login session" >&2
            exit 1
        fi
        IP="$(cat "$SESSION_FILE")"
        URI=""
        for arg in "$@"; do
            case "$arg" in
                -*) ;;
                *)
                    URI="$arg"
                    break
                    ;;
            esac
        done
        if [ -z "$URI" ]; then
            echo "MOCK ILOREST ERROR: rawget missing URI" >&2
            exit 1
        fi
        log_call "rawget|${IP}|${URI}|${CACHE_DIR}"
        MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL="*" "$PY" "$(to_py "$MOCK_LIB_DIR/mock_redfish.py")" "$IP" "$URI"
        exit $?
        ;;
    rawput|rawpost)
        if [ ! -f "$SESSION_FILE" ]; then
            echo "MOCK ILOREST ERROR: $SUBCMD called without active login session" >&2
            exit 1
        fi
        IP="$(cat "$SESSION_FILE")"
        PAYLOAD_FILE=""
        for arg in "$@"; do
            case "$arg" in
                -*) ;;
                *)
                    PAYLOAD_FILE="$arg"
                    break
                    ;;
            esac
        done
        if [ -z "$PAYLOAD_FILE" ] || [ ! -f "$PAYLOAD_FILE" ]; then
            echo "MOCK ILOREST ERROR: $SUBCMD payload file missing: $PAYLOAD_FILE" >&2
            exit 1
        fi
        SAFE_IP="$(printf '%s' "$IP" | tr '.:' '__')"
        SEQ_TAG="$$_${RANDOM}"
        AUDIT_PAYLOAD=""
        if [ -n "$AUDIT_DIR" ]; then
            AUDIT_PAYLOAD="$AUDIT_DIR/${SUBCMD}_${SAFE_IP}_${SEQ_TAG}.json"
            cp "$PAYLOAD_FILE" "$AUDIT_PAYLOAD"
            AUDIT_PAYLOAD="$(to_py "$AUDIT_PAYLOAD")"
        fi
        REQ_PATH="$("$PY" -c "import sys, json; d=json.load(open(sys.argv[1])); print(d.get('path',''))" "$(to_py "$PAYLOAD_FILE")" 2>/dev/null || true)"
        if [ -z "$REQ_PATH" ]; then
            echo "MOCK ILOREST ERROR: $SUBCMD JSON missing 'path' key" >&2
            exit 1
        fi
        log_call "${SUBCMD}|${IP}|${REQ_PATH}|${AUDIT_PAYLOAD}|${CACHE_DIR}"
        if [ "$SUBCMD" = "rawput" ]; then
            printf '{"@Message.ExtendedInfo": [{"MessageId": "iLO.2.15.SystemResetRequired"}], "Message": "SystemResetRequired"}\n'
        else
            printf '{"@odata.id": "%s/2", "Id": "2", "Status": {"State": "Enabled", "Health": "OK"}}\n' "$REQ_PATH"
        fi
        exit 0
        ;;
    rawdelete)
        if [ ! -f "$SESSION_FILE" ]; then
            echo "MOCK ILOREST ERROR: rawdelete called without active login session" >&2
            exit 1
        fi
        IP="$(cat "$SESSION_FILE")"
        URI=""
        for arg in "$@"; do
            case "$arg" in
                -*) ;;
                *)
                    URI="$arg"
                    break
                    ;;
            esac
        done
        if [ -z "$URI" ]; then
            echo "MOCK ILOREST ERROR: rawdelete missing URI" >&2
            exit 1
        fi
        log_call "rawdelete|${IP}|${URI}|${CACHE_DIR}"
        if [ "$IP" = "10.10.0.4" ]; then
            echo "An invalid response body was returned: No JSON object could be decoded (405 Method Not Allowed on HBA/None)" >&2
            exit 1
        fi
        printf '{"Message": "Deleted %s"}\n' "$URI"
        exit 0
        ;;
    reboot)
        if [ ! -f "$SESSION_FILE" ]; then
            echo "MOCK ILOREST ERROR: reboot called without active login session" >&2
            exit 1
        fi
        IP="$(cat "$SESSION_FILE")"
        log_call "reboot|${IP}|${CACHE_DIR}"
        echo "Server reboot initiated."
        exit 0
        ;;
    *)
        echo "MOCK ILOREST ERROR: unknown subcommand '$SUBCMD'" >&2
        exit 1
        ;;
esac
EOF
chmod +x "$MOCK_BIN/ilorest"

export PATH="$MOCK_BIN:$PATH"
export MOCK_LIB_DIR="$MOCK_LIB"

# Helper to run raid0.sh in an isolated test case directory
run_case() {
    local case_name="$1"
    local hosts_content="$2"
    local stdin_content="$3"
    shift 3

    CASE_DIR="$WORK_ROOT/$case_name"
    rm -rf "$CASE_DIR"
    mkdir -p "$CASE_DIR/audit" "$CASE_DIR/run"
    printf '%s\n' "$hosts_content" > "$CASE_DIR/worklist.txt"

    export MOCK_AUDIT_DIR="$CASE_DIR/audit"
    : > "$MOCK_AUDIT_DIR/calls.log"
    : > "$MOCK_AUDIT_DIR/errors.log"

    LAST_OUT_FILE="$CASE_DIR/stdout.log"
    (
        cd "$CASE_DIR/run" || exit 1
        printf '%s' "$stdin_content" | bash "$ILO_DIR/raid0.sh" -w "$CASE_DIR/worklist.txt" "$@"
    ) > "$LAST_OUT_FILE" 2>&1
    LAST_RC=$?
    LAST_OUT="$(cat "$LAST_OUT_FILE")"

    # Also clean up any logs written directly into $ILO_DIR/logs if script writes relative to SCRIPT_DIR
    if [ -d "$ILO_DIR/logs" ]; then
        cp -r "$ILO_DIR/logs" "$CASE_DIR/script_logs" 2>/dev/null || true
        rm -rf "$ILO_DIR/logs"
    fi
}

echo "================================================================"
echo " HPE iLO Multi-Server RAID Tool - Integration & Static Test Suite"
echo "================================================================"

# ===========================================================================
# 1. Static Compatibility Checks
# ===========================================================================
echo ""
echo "--- [1] Static Compatibility Checks ---"

# 1a. bash -n syntax check
for sh_file in "$ILO_DIR/raid0.sh" "$ILO_DIR/lib/ilo_worker.sh" "$ILO_DIR/tests/test_raid_tool.sh"; do
    if [ ! -f "$sh_file" ]; then
        fail "Missing shell script: $sh_file"
    elif bash -n "$sh_file" >/dev/null 2>&1; then
        pass "bash -n syntax valid: $(basename "$sh_file")"
    else
        fail "bash -n syntax error in: $sh_file"
    fi
done

# 1b. Zero CRLF in all .sh, .py, .txt, .md, .gitattributes, worklist.txt.example
CRLF_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$ILO_DIR")" <<'PYEOF'
import os, sys
root = sys.argv[1]
bad = []
for dirpath, dirnames, filenames in os.walk(root):
    for fn in filenames:
        if fn.endswith((".sh", ".py", ".txt", ".md", ".example")) or fn == ".gitattributes":
            p = os.path.join(dirpath, fn)
            with open(p, "rb") as fh:
                data = fh.read()
            if b"\r" in data:
                bad.append(os.path.relpath(p, root))
if bad:
    print("CRLF:" + ",".join(bad))
else:
    print("OK")
PYEOF
)"
assert_eq "$CRLF_CHECK" "OK" "Zero CRLF (\\r) in all .sh, .py, .txt, .md, .example, .gitattributes files"

# 1c. Zero Bash 4.1 forbidden constructs in .sh files (excluding comments/test check literals)
BASH41_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$ILO_DIR")" <<'PYEOF'
import os, re, sys
root = sys.argv[1]
patterns = [
    ("mapfile", re.compile(r'(^|[;&|\s])mapfile(\s|$)')),
    ("readarray", re.compile(r'(^|[;&|\s])readarray(\s|$)')),
    ("local -n", re.compile(r'\blocal\s+-n\b')),
    ("wait -n", re.compile(r'\bwait\s+-n\b')),
    ("declare -g", re.compile(r'\bdeclare\s+-[a-zA-Z]*g\b')),
    ("@Q", re.compile(r'\$\{[^}]+@Q\}')),
]
bad = []
for rel in ["raid0.sh", os.path.join("lib", "ilo_worker.sh"), os.path.join("tests", "test_raid_tool.sh")]:
    p = os.path.join(root, rel)
    if not os.path.exists(p):
        bad.append("missing:" + rel)
        continue
    with open(p, "r") as fh:
        in_py = False
        for lineno, line in enumerate(fh, 1):
            stripped = line.strip()
            if stripped.startswith("<<'PYEOF'") or "<<'PYEOF'" in stripped:
                in_py = True
                continue
            if in_py:
                if stripped == "PYEOF":
                    in_py = False
                continue
            if stripped.startswith("#"):
                continue
            for name, pat in patterns:
                if pat.search(line):
                    bad.append("%s:%d:%s" % (rel, lineno, name))
if bad:
    print("FORBIDDEN:" + ";".join(bad))
else:
    print("OK")
PYEOF
)"
assert_eq "$BASH41_CHECK" "OK" "Zero Bash 4.1 forbidden constructs in all shell scripts"

# 1d. Python AST check for Python 2.6.6 compatibility on lib/raid_parser.py
PY26_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$ILO_DIR/lib/raid_parser.py")" <<'PYEOF'
import ast, re, sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    src = fh.read()

tree = ast.parse(src, filename=path)
errors = []

FORBIDDEN_NODES = (
    "JoinedStr",
    "FormattedValue",
    "DictComp",
    "SetComp",
    "AnnAssign",
    "Nonlocal",
    "YieldFrom",
    "AsyncFunctionDef",
    "Await",
    "NamedExpr",
)

for node in ast.walk(tree):
    cname = type(node).__name__
    if cname in FORBIDDEN_NODES:
        errors.append("line %d: forbidden syntax %s" % (getattr(node, "lineno", 0), cname))
    if isinstance(node, ast.With) and len(node.items) > 1:
        errors.append("line %d: multi-item with statement" % node.lineno)
    if isinstance(node, ast.FunctionDef):
        if getattr(node, "returns", None) is not None:
            errors.append("line %d: return type annotation" % node.lineno)
        for arg in node.args.args:
            if getattr(arg, "annotation", None) is not None:
                errors.append("line %d: arg type annotation" % node.lineno)
    if isinstance(node, ast.Import):
        for alias in node.names:
            if alias.name in ("argparse", "OrderedDict"):
                errors.append("line %d: forbidden import %s" % (node.lineno, alias.name))
    if isinstance(node, ast.ImportFrom):
        mod = node.module or ""
        if mod == "argparse":
            errors.append("line %d: forbidden import from argparse" % node.lineno)
        for alias in node.names:
            if alias.name == "OrderedDict":
                errors.append("line %d: forbidden import OrderedDict" % node.lineno)
    if isinstance(node, ast.Attribute) and node.attr in ("OrderedDict", "check_output"):
        errors.append("line %d: forbidden attribute %s" % (node.lineno, node.attr))
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr == "format":
        if isinstance(node.func.value, ast.Constant) and isinstance(node.func.value.value, str):
            s = node.func.value.value
            if re.search(r'(?<!\{)\{(?:![rsa])?(?::[^{}]*)?\}', s):
                errors.append("line %d: unindexed {}.format() call" % node.lineno)

if "from __future__ import print_function" not in src:
    errors.append("missing 'from __future__ import print_function'")

if errors:
    print("PY26_ERR:" + " | ".join(errors))
else:
    print("OK")
PYEOF
)"
assert_eq "$PY26_CHECK" "OK" "Python 2.6.6 AST compatibility check on lib/raid_parser.py"

# 1e. Inline python -c PEP-263 check in shell scripts (Python 2.6.6 requires coding header for non-ASCII)
PEP263_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$ILO_DIR")" <<'PYEOF'
import os, re, sys
root = sys.argv[1]
bad = []
for rel in ["raid0.sh", os.path.join("lib", "ilo_worker.sh")]:
    p = os.path.join(root, rel)
    with open(p, "rb") as fh:
        content = fh.read().decode("utf-8", "replace")
    for m in re.finditer(r'"\$PYTHON_BIN"\s+-c\s+\'(.*?)\'', content, re.S):
        snippet = m.group(1)
        if any(ord(ch) > 127 for ch in snippet):
            first2 = "\n".join(snippet.splitlines()[:2])
            if not re.search(r"coding[:=]\s*utf-?8", first2, re.I):
                lineno = content[:m.start()].count("\n") + 1
                bad.append("%s:%d:non-ASCII inline python -c without PEP-263 header" % (rel, lineno))
if bad:
    print("PEP263_ERR:" + ";".join(bad))
else:
    print("OK")
PYEOF
)"
assert_eq "$PEP263_CHECK" "OK" "PEP-263 UTF-8 coding header check on inline python -c blocks"

# 1f. Verify all pdsh -R exec worker calls in raid0.sh invoke via `bash` (no +x required on ilo_worker.sh)
PDSH_BASH_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$ILO_DIR/raid0.sh")" <<'PYEOF'
import re, sys
with open(sys.argv[1], "rb") as fh:
    content = fh.read().decode("utf-8", "replace")
pdsh_lines = [ln.strip() for ln in content.splitlines() if re.search(r'\bpdsh\s+-R\s+exec\b', ln) and not ln.strip().startswith("#")]
if len(pdsh_lines) < 3:
    print("FAIL: expected >=3 pdsh -R exec lines (discover/apply/reboot), found %d" % len(pdsh_lines))
    sys.exit(0)
bad = [ln for ln in pdsh_lines if not re.search(r'\bbash\s+"\$WORKER', ln)]
if bad:
    print("FAIL: pdsh calls without bash prefix: " + " | ".join(bad))
else:
    print("OK")
PYEOF
)"
assert_eq "$PDSH_BASH_CHECK" "OK" "All pdsh -R exec calls in raid0.sh invoke worker via bash (no +x bit required)"

# ===========================================================================
# 2. Gen10 SmartStorageConfig Tests (10.10.0.1)
# ===========================================================================
echo ""
echo "--- [2] Gen10 SmartStorageConfig Tests (10.10.0.1) ---"

# 2a. Combined RAID 0 on sda-sdh + reboot=y
run_case "gen10_raid0_combined_reboot_y" "10.10.0.1" \
    "Administrator
${TEST_SECRET}
auto
sda-sdh
0
1
y
y
"
assert_eq "$LAST_RC" "0" "Gen10 combined RAID 0 exited 0"
assert_eq "$(wc -l < "$MOCK_AUDIT_DIR/errors.log" | tr -d ' ')" "0" "Gen10 combined RAID 0 zero mock ilorest errors"

GEN10_VERIFY="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" "$(to_py_path "$LAST_OUT_FILE")" <<'PYEOF'
import os, sys, json, glob

audit_dir, out_file = sys.argv[1], sys.argv[2]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
with open(out_file, "r", encoding="utf-8", errors="replace") as fh:
    out = fh.read()

puts = [c for c in calls if c.startswith("rawput|10.10.0.1|")]
if len(puts) != 1:
    print("FAIL: expected 1 rawput, got %d" % len(puts))
    sys.exit(0)
parts = puts[0].split("|")
path = parts[2].rstrip("/")
if path != "/redfish/v1/Systems/1/smartstorageconfig/settings":
    print("FAIL: unexpected rawput path: %s" % path)
    sys.exit(0)
payload_file = parts[3]
with open(payload_file, "r") as fh:
    payload = json.load(fh)
body = payload.get("body", {})
if body.get("DataGuard") != "Disabled":
    print("FAIL: DataGuard != Disabled: %r" % (body.get("DataGuard"),))
    sys.exit(0)
lds = body.get("LogicalDrives", [])
if len(lds) != 2:
    print("FAIL: expected 2 LogicalDrives (OS + new Raid0), got %d: %r" % (len(lds), lds))
    sys.exit(0)
os_preserved = any(
    ld.get("Raid") == "Raid1" and sorted(ld.get("DataDrives", [])) == ["1I:1:10", "1I:1:9"]
    for ld in lds
)
expected_8 = ["1I:1:%d" % i for i in range(1, 9)]
new_raid0 = any(
    ld.get("Raid") == "Raid0" and sorted(ld.get("DataDrives", [])) == sorted(expected_8)
    for ld in lds
)
if not os_preserved:
    print("FAIL: existing OS RAID1 (1I:1:9, 1I:1:10) was NOT preserved!")
    sys.exit(0)
if not new_raid0:
    print("FAIL: new Raid0 with 1I:1:1..1I:1:8 not found: %r" % (lds,))
    sys.exit(0)

# Verify reboot-required server list printed BEFORE reboot prompt
prompt_idx = out.find("재부팅하시겠습니까")
if prompt_idx == -1:
    print("FAIL: reboot prompt not found in output")
    sys.exit(0)
before_prompt = out[:prompt_idx]
if "10.10.0.1" not in before_prompt or "ProLiant DL380 Gen10" not in before_prompt:
    print("FAIL: reboot server info (10.10.0.1 / ProLiant DL380 Gen10) not printed BEFORE reboot prompt")
    sys.exit(0)

reboots = [c for c in calls if c.startswith("reboot|10.10.0.1|")]
if len(reboots) != 1:
    print("FAIL: expected 1 reboot call on 'y', got %d" % len(reboots))
    sys.exit(0)

# Verify Gen10 read-only /Storage mirror was deduplicated (only 1 controller in menu: [1], no [2])
if "[2]" in out:
    print("FAIL: Gen10 /Storage stub was not deduplicated (found controller [2] in menu)")
    sys.exit(0)

print("OK")
PYEOF
)"
assert_eq "$GEN10_VERIFY" "OK" "Gen10 combined RAID 0 payload (DataGuard=Disabled, OS RAID1 preserved, 8-disk Raid0, reboot list before prompt + reboot called)"

# 2b. Per-disk RAID 0 (1 disk = 1 volume) + reboot=n
run_case "gen10_raid0_perdisk_reboot_n" "10.10.0.1" \
    "Administrator
${TEST_SECRET}
auto
sda-sdh
0
2
y
n
"
assert_eq "$LAST_RC" "0" "Gen10 per-disk RAID 0 exited 0"

GEN10_PERDISK="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys, json

audit_dir = sys.argv[1]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
puts = [c for c in calls if c.startswith("rawput|10.10.0.1|")]
if len(puts) != 1:
    print("FAIL: expected 1 rawput, got %d" % len(puts))
    sys.exit(0)
with open(puts[0].split("|")[3], "r") as fh:
    body = json.load(fh).get("body", {})
lds = body.get("LogicalDrives", [])
if len(lds) != 9:
    print("FAIL: expected 9 LogicalDrives (1 OS + 8 per-disk Raid0), got %d" % len(lds))
    sys.exit(0)
r0 = [ld for ld in lds if ld.get("Raid") == "Raid0" and len(ld.get("DataDrives", [])) == 1]
r1 = [ld for ld in lds if ld.get("Raid") == "Raid1" and sorted(ld.get("DataDrives", [])) == ["1I:1:10", "1I:1:9"]]
if len(r0) != 8 or len(r1) != 1:
    print("FAIL: expected 8 single-disk Raid0 + 1 OS Raid1, got r0=%d r1=%d" % (len(r0), len(r1)))
    sys.exit(0)
reboots = [c for c in calls if c.startswith("reboot|")]
if len(reboots) != 0:
    print("FAIL: expected 0 reboot calls when answering 'n', got %d" % len(reboots))
    sys.exit(0)
print("OK")
PYEOF
)"
assert_eq "$GEN10_PERDISK" "OK" "Gen10 per-disk RAID 0 creates 8 separate Raid0 + preserves OS RAID1 + reboot=n skips reboot"

# 2c. RAID 5 on Gen10
run_case "gen10_raid5" "10.10.0.1" \
    "Administrator
${TEST_SECRET}
auto
sda-sdh
5
y
n
"
assert_eq "$LAST_RC" "0" "Gen10 RAID 5 exited 0"

GEN10_R5="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys, json
audit_dir = sys.argv[1]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
puts = [c for c in calls if c.startswith("rawput|10.10.0.1|")]
if len(puts) != 1:
    print("FAIL: expected 1 rawput, got %d" % len(puts))
    sys.exit(0)
with open(puts[0].split("|")[3], "r") as fh:
    lds = json.load(fh).get("body", {}).get("LogicalDrives", [])
r5 = [ld for ld in lds if ld.get("Raid") == "Raid5" and len(ld.get("DataDrives", [])) == 8]
if len(r5) != 1:
    print("FAIL: expected 1 8-disk Raid5 entry, got %r" % (lds,))
    sys.exit(0)
print("OK")
PYEOF
)"
assert_eq "$GEN10_R5" "OK" "Gen10 RAID 5 payload has Raid5 with 8 drives"

# ===========================================================================
# 3. Gen10 Plus Redfish Storage + Conflict & --force-delete Tests (10.10.0.2)
# ===========================================================================
echo ""
echo "--- [3] Gen10 Plus Redfish Storage & Conflict Tests (10.10.0.2) ---"

# 3a. Without --force-delete: detects conflict, prints --force-delete guidance, does NOT modify server
run_case "gen10plus_conflict_no_force" "10.10.0.2" \
    "Administrator
${TEST_SECRET}
auto
nvme0n1-nvme7n1
0
1
"
assert_contains "$LAST_OUT" "[BOOT" "Gen10 Plus marks NS204i-p boot controller with [BOOT"
assert_contains "$LAST_OUT" "MR416i-a" "Gen10 Plus lists/selects MR416i-a controller"
assert_contains "$LAST_OUT" "--force-delete" "Gen10 Plus conflict prints explicit --force-delete option guidance"
if [ "$LAST_RC" -ne 0 ]; then
    pass "Gen10 Plus conflict without --force-delete aborts with non-zero exit ($LAST_RC)"
else
    fail "Gen10 Plus conflict without --force-delete should exit non-zero"
fi

NO_WRITES_3A="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys
with open(os.path.join(sys.argv[1], "calls.log"), "r") as fh:
    writes = [l.strip() for l in fh if l.startswith(("rawput|", "rawpost|", "rawdelete|", "reboot|"))]
print("OK" if not writes else "WRITES:" + str(len(writes)))
PYEOF
)"
assert_eq "$NO_WRITES_3A" "OK" "Gen10 Plus conflict without --force-delete performs ZERO write API calls"

# 3b. With --force-delete: rejects 'y' (requires exact 'DELETE')
run_case "gen10plus_force_reject_y" "10.10.0.2" \
    "Administrator
${TEST_SECRET}
auto
nvme0n1-nvme7n1
0
1
y
" --force-delete

NO_WRITES_3B="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys
with open(os.path.join(sys.argv[1], "calls.log"), "r") as fh:
    writes = [l.strip() for l in fh if l.startswith(("rawput|", "rawpost|", "rawdelete|", "reboot|"))]
print("OK" if not writes else "WRITES:" + str(len(writes)))
PYEOF
)"
assert_eq "$NO_WRITES_3B" "OK" "Gen10 Plus --force-delete rejects 'y' confirmation and performs ZERO writes"

# 3c. With --force-delete + exact 'DELETE': rawdelete then rawpost RAID0
run_case "gen10plus_force_delete_raid0" "10.10.0.2" \
    "Administrator
${TEST_SECRET}
auto
nvme0n1-nvme7n1
0
1
DELETE
" --force-delete
assert_eq "$LAST_RC" "0" "Gen10 Plus --force-delete RAID 0 with DELETE exited 0"

G10P_R0="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys, json
audit_dir = sys.argv[1]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
del_idx = [i for i, c in enumerate(calls) if c.startswith("rawdelete|10.10.0.2|/redfish/v1/Systems/1/Storage/DE040000/Volumes/1")]
post_idx = [i for i, c in enumerate(calls) if c.startswith("rawpost|10.10.0.2|/redfish/v1/Systems/1/Storage/DE040000/Volumes")]
if len(del_idx) != 1 or len(post_idx) != 1:
    print("FAIL: expected 1 delete and 1 post, got del=%d post=%d" % (len(del_idx), len(post_idx)))
    sys.exit(0)
if del_idx[0] >= post_idx[0]:
    print("FAIL: rawdelete must happen BEFORE rawpost")
    sys.exit(0)
payload_file = calls[post_idx[0]].split("|")[3]
with open(payload_file, "r") as fh:
    body = json.load(fh).get("body", {})
if body.get("RAIDType") != "RAID0":
    print("FAIL: expected RAIDType=RAID0, got %r" % (body.get("RAIDType"),))
    sys.exit(0)
drives = body.get("Links", {}).get("Drives", [])
if len(drives) != 8:
    print("FAIL: expected 8 drives in Links.Drives, got %d" % len(drives))
    sys.exit(0)
print("OK")
PYEOF
)"
assert_eq "$G10P_R0" "OK" "Gen10 Plus --force-delete DELETE runs rawdelete on conflicting vol then rawpost RAID0 (8 NVMe)"

# 3d. With --force-delete + RAID 10
run_case "gen10plus_force_delete_raid10" "10.10.0.2" \
    "Administrator
${TEST_SECRET}
auto
nvme0n1-nvme7n1
10
DELETE
" --force-delete
assert_eq "$LAST_RC" "0" "Gen10 Plus --force-delete RAID 10 exited 0"

G10P_R10="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys, json
audit_dir = sys.argv[1]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
posts = [c for c in calls if c.startswith("rawpost|10.10.0.2|/redfish/v1/Systems/1/Storage/DE040000/Volumes")]
if len(posts) != 1:
    print("FAIL: expected 1 post, got %d" % len(posts))
    sys.exit(0)
with open(posts[0].split("|")[3], "r") as fh:
    body = json.load(fh).get("body", {})
if body.get("RAIDType") != "RAID10":
    print("FAIL: expected RAIDType=RAID10, got %r" % (body.get("RAIDType"),))
    sys.exit(0)
print("OK")
PYEOF
)"
assert_eq "$G10P_R10" "OK" "Gen10 Plus --force-delete RAID 10 posts RAIDType=RAID10"

# ===========================================================================
# 4. Gen11 Mixed SAS+NVMe Protocol Mapping Tests (10.10.0.3)
# ===========================================================================
echo ""
echo "--- [4] Gen11 Mixed SAS+NVMe Protocol Mapping Tests (10.10.0.3) ---"

verify_gen11_drives() {
    local audit_dir="$1"
    local expected_prefix="$2"
    local expected_count="$3"
    "$PYTHON_BIN" - "$(to_py_path "$audit_dir")" "$expected_prefix" "$expected_count" <<'PYEOF'
import os, sys, json
audit_dir, prefix, count = sys.argv[1], sys.argv[2], int(sys.argv[3])
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
posts = [c for c in calls if c.startswith("rawpost|10.10.0.3|/redfish/v1/Systems/1/Storage/DE041000/Volumes")]
if len(posts) != 1:
    print("FAIL: expected 1 post, got %d" % len(posts))
    sys.exit(0)
with open(posts[0].split("|")[3], "r") as fh:
    body = json.load(fh).get("body", {})
drives = [d.get("@odata.id", "") for d in body.get("Links", {}).get("Drives", [])]
if len(drives) != count:
    print("FAIL: expected %d drives, got %d (%r)" % (count, len(drives), drives))
    sys.exit(0)
expected = ["/redfish/v1/Systems/1/Storage/DE041000/Drives/%s%d" % (prefix, i) for i in range(1, count + 1)]
if sorted(drives) != sorted(expected):
    print("FAIL: expected %r, got %r" % (expected, drives))
    sys.exit(0)
print("OK")
PYEOF
}

# 4a. sda-sdh -> strictly 8 SAS drives
run_case "gen11_sda_sdh" "10.10.0.3" \
    "Administrator
${TEST_SECRET}
auto
sda-sdh
0
1
y
"
assert_eq "$LAST_RC" "0" "Gen11 sda-sdh exited 0"
assert_eq "$(verify_gen11_drives "$MOCK_AUDIT_DIR" "sas" 8)" "OK" "Gen11 sda-sdh selects strictly 8 SAS drives (sas1..sas8)"

# 4b. nvme0n1-nvme7n1 -> strictly 8 NVMe drives
run_case "gen11_nvme0_nvme7" "10.10.0.3" \
    "Administrator
${TEST_SECRET}
auto
nvme0n1-nvme7n1
0
1
y
"
assert_eq "$LAST_RC" "0" "Gen11 nvme0n1-nvme7n1 exited 0"
assert_eq "$(verify_gen11_drives "$MOCK_AUDIT_DIR" "nvme" 8)" "OK" "Gen11 nvme0n1-nvme7n1 selects strictly 8 NVMe drives (nvme1..nvme8)"

# 4c. 1-8 -> index 1..8 (1I:1:1..1I:1:8 = sas1..sas8)
run_case "gen11_index_1_8" "10.10.0.3" \
    "Administrator
${TEST_SECRET}
auto
1-8
0
1
y
"
assert_eq "$LAST_RC" "0" "Gen11 1-8 exited 0"
assert_eq "$(verify_gen11_drives "$MOCK_AUDIT_DIR" "sas" 8)" "OK" "Gen11 1-8 selects first 8 drives by natural location order (sas1..sas8)"

# 4d. Physical range 1I:1:1-1I:1:8
run_case "gen11_phys_range" "10.10.0.3" \
    "Administrator
${TEST_SECRET}
auto
1I:1:1-1I:1:8
0
1
y
"
assert_eq "$LAST_RC" "0" "Gen11 1I:1:1-1I:1:8 exited 0"
assert_eq "$(verify_gen11_drives "$MOCK_AUDIT_DIR" "sas" 8)" "OK" "Gen11 1I:1:1-1I:1:8 resolves exact 8 physical SAS locations"

# 4e. Gen11 SR932i-p HBA/Raw (RAIDType='None', DELETE returns 405): must NOT attempt rawdelete, POSTs directly!
run_case "gen11_hba_raw_none" "10.10.0.4" \
    "Administrator
${TEST_SECRET}
auto
sdb-sdh
0
1
y
"
assert_eq "$LAST_RC" "0" "Gen11 SR932i-p HBA/Raw (RAIDType=None) exited 0 without --force-delete"
assert_contains "$LAST_OUT" "[HBA/Raw]" "Gen11 SR932i-p HBA/Raw drives labeled [HBA/Raw] in drive table"

G11_HBA_VERIFY="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" <<'PYEOF'
import os, sys, json
audit_dir = sys.argv[1]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]
dels = [c for c in calls if c.startswith("rawdelete|10.10.0.4|")]
posts = [c for c in calls if c.startswith("rawpost|10.10.0.4|/redfish/v1/Systems/1/Storage/DE009000/Volumes")]
if dels:
    print("FAIL: must NOT call rawdelete on RAIDType=None HBA/Raw volumes, got %d" % len(dels))
    sys.exit(0)
if len(posts) != 1:
    print("FAIL: expected 1 rawpost, got %d" % len(posts))
    sys.exit(0)
with open(posts[0].split("|")[3], "r") as fh:
    body = json.load(fh).get("body", {})
drives = [d.get("@odata.id", "") for d in body.get("Links", {}).get("Drives", [])]
expected = ["/redfish/v1/Systems/1/Storage/DE009000/Drives/%d" % i for i in range(2, 9)]
if body.get("RAIDType") != "RAID0" or sorted(drives) != sorted(expected):
    print("FAIL: unexpected payload RAIDType=%r drives=%r" % (body.get("RAIDType"), drives))
    sys.exit(0)
print("OK")
PYEOF
)"
assert_eq "$G11_HBA_VERIFY" "OK" "Gen11 SR932i-p HBA/Raw skips rawdelete (0 deletes) and POSTs RAID0 on sdb-sdh (Drives 2..8)"

# ===========================================================================
# 5. Heterogeneous Multi-Server Batch (10.10.0.1 + 10.10.0.2 + 10.10.0.3) & --dry-run
# ===========================================================================
echo ""
echo "--- [5] Heterogeneous Multi-Server Batch & --dry-run ---"

run_case "multi_hetero_dry_run" "10.10.0.1
10.10.0.2
10.10.0.3" \
    "Administrator
${TEST_SECRET}
auto
1-8
0
1
" --dry-run --force-delete
assert_eq "$LAST_RC" "0" "Multi-server heterogeneous --dry-run exited 0"
assert_eq "$(wc -l < "$MOCK_AUDIT_DIR/errors.log" | tr -d ' ')" "0" "Multi-server parallel pdsh zero mock ilorest errors (cache-dir isolation)"

MULTI_CHECK="$("$PYTHON_BIN" - "$(to_py_path "$MOCK_AUDIT_DIR")" "$(to_py_path "$CASE_DIR")" "$TEST_SECRET" <<'PYEOF'
import os, sys

audit_dir, case_dir, secret = sys.argv[1], sys.argv[2], sys.argv[3]
with open(os.path.join(audit_dir, "calls.log"), "r") as fh:
    calls = [l.strip() for l in fh if l.strip()]

# 1. Zero write API calls under --dry-run
writes = [c for c in calls if c.startswith(("rawput|", "rawpost|", "rawdelete|", "reboot|"))]
if writes:
    print("FAIL: --dry-run made write API calls: %r" % (writes,))
    sys.exit(0)

# 2. Unique --cache-dir per IP across parallel workers
ip_caches = {}
for c in calls:
    parts = c.split("|")
    if parts[0] == "login":
        ip, cdir = parts[1], parts[2]
        ip_caches.setdefault(ip, set()).add(cdir)

if sorted(ip_caches.keys()) != ["10.10.0.1", "10.10.0.2", "10.10.0.3"]:
    print("FAIL: expected logins for all 3 IPs, got %r" % (sorted(ip_caches.keys()),))
    sys.exit(0)

all_cdirs = []
for ip, cset in ip_caches.items():
    for d in cset:
        all_cdirs.append((ip, d))
unique_dirs = set(d for _, d in all_cdirs)
if len(unique_dirs) < 3:
    print("FAIL: cache dirs not unique across IPs: %r" % (ip_caches,))
    sys.exit(0)

# 3. Password MUST NEVER appear in stdout or any log file under case_dir
for dirpath, _, filenames in os.walk(case_dir):
    for fn in filenames:
        p = os.path.join(dirpath, fn)
        with open(p, "rb") as fh:
            raw = fh.read()
        if secret.encode("utf-8") in raw:
            print("FAIL: password leaked in %s" % p)
            sys.exit(0)

# 4. Heterogeneity warning printed in stdout
with open(os.path.join(case_dir, "stdout.log"), "r", encoding="utf-8", errors="replace") as fh:
    out = fh.read()
if not any(k in out for k in ("이종", "혼합", "상이", "다른", "경고", "WARNING")):
    print("FAIL: heterogeneity warning not found in output")
    sys.exit(0)

print("OK")
PYEOF
)"
assert_eq "$MULTI_CHECK" "OK" "Multi-server batch: heterogeneity warning, unique --cache-dir per IP, zero password leak, zero --dry-run writes"

# ===========================================================================
# Summary
# ===========================================================================
echo ""
echo "================================================================"
printf 'TOTAL: %d PASSED, %d FAILED\n' "$PASS_COUNT" "$FAIL_COUNT"
echo "================================================================"

if [ "$FAIL_COUNT" -ne 0 ]; then
    exit 1
fi
exit 0
