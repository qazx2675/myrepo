#!/usr/bin/python3
# mkrepo.py <디렉토리> - createrepo_c 가 없을 때 쓰는 최소 repodata 생성기 (gpu_install 테스트 전용).
# 디렉토리 아래 *.rpm 으로 primary/filelists/other.xml.gz 와 repomd.xml 을 만든다.
import gzip, hashlib, os, sys, time
from xml.sax.saxutils import escape, quoteattr
import rpm

FLAGS = {rpm.RPMSENSE_EQUAL: "EQ", rpm.RPMSENSE_LESS: "LT", rpm.RPMSENSE_GREATER: "GT",
         rpm.RPMSENSE_LESS | rpm.RPMSENSE_EQUAL: "LE", rpm.RPMSENSE_GREATER | rpm.RPMSENSE_EQUAL: "GE"}


def s(v):
    if isinstance(v, bytes):
        return v.decode("utf-8", "replace")
    return "" if v is None else str(v)


def deps(h, ntag, ftag, vtag, tag):
    out = []
    for n, f, v in zip(h[ntag], h[ftag], h[vtag]):
        n, v = s(n), s(v)
        if n.startswith("rpmlib("):
            continue
        a = "name=%s" % quoteattr(n)
        fl = FLAGS.get(f & (rpm.RPMSENSE_EQUAL | rpm.RPMSENSE_LESS | rpm.RPMSENSE_GREATER))
        if fl and v:
            e, _, vr = v.rpartition(":")
            ver, _, rel = vr.partition("-")
            a += ' flags="%s" epoch="%s" ver=%s' % (fl, e or "0", quoteattr(ver))
            if rel:
                a += " rel=%s" % quoteattr(rel)
        out.append("<rpm:entry %s/>" % a)
    return "<rpm:%s>%s</rpm:%s>" % (tag, "".join(out), tag) if out else ""


def main(top):
    ts = rpm.TransactionSet()
    ts.setVSFlags(-1)
    prim, fl, oth = [], [], []
    for d, _, fs in os.walk(top):
        if "repodata" in d.split(os.sep):
            continue
        for f in sorted(fs):
            if not f.endswith(".rpm") or f.endswith(".src.rpm"):
                continue
            p = os.path.join(d, f)
            rel = os.path.relpath(p, top)
            data = open(p, "rb").read()
            pkgid = hashlib.sha256(data).hexdigest()
            fd = os.open(p, os.O_RDONLY)
            h = ts.hdrFromFdno(fd)
            os.close(fd)
            name, arch = s(h[rpm.RPMTAG_NAME]), s(h[rpm.RPMTAG_ARCH])
            ver, rl = s(h[rpm.RPMTAG_VERSION]), s(h[rpm.RPMTAG_RELEASE])
            ep = s(h[rpm.RPMTAG_EPOCH]) or "0"
            files = [s(x) for x in h[rpm.RPMTAG_FILENAMES]]
            fx = "".join("<file>%s</file>" % escape(x) for x in files)
            pv = '<version epoch="%s" ver=%s rel=%s/>' % (ep, quoteattr(ver), quoteattr(rl))
            prim.append(
                '<package type="rpm"><name>%s</name><arch>%s</arch>%s'
                '<checksum type="sha256" pkgid="YES">%s</checksum>'
                "<summary>%s</summary><description>%s</description><packager/><url/>"
                '<time file="%d" build="%d"/><size package="%d" installed="%d" archive="0"/>'
                "<location href=%s/><format><rpm:license>%s</rpm:license><rpm:vendor/>"
                "<rpm:group>Unspecified</rpm:group><rpm:buildhost>%s</rpm:buildhost>"
                "<rpm:sourcerpm>%s</rpm:sourcerpm>%s%s%s</format></package>"
                % (escape(name), arch, pv, pkgid, escape(s(h[rpm.RPMTAG_SUMMARY])),
                   escape(s(h[rpm.RPMTAG_DESCRIPTION])), int(os.path.getmtime(p)),
                   int(h[rpm.RPMTAG_BUILDTIME] or 0), len(data), int(h[rpm.RPMTAG_SIZE] or 0),
                   quoteattr(rel), escape(s(h[rpm.RPMTAG_LICENSE])), escape(s(h[rpm.RPMTAG_BUILDHOST])),
                   escape(s(h[rpm.RPMTAG_SOURCERPM])),
                   deps(h, rpm.RPMTAG_PROVIDENAME, rpm.RPMTAG_PROVIDEFLAGS, rpm.RPMTAG_PROVIDEVERSION, "provides"),
                   deps(h, rpm.RPMTAG_REQUIRENAME, rpm.RPMTAG_REQUIREFLAGS, rpm.RPMTAG_REQUIREVERSION, "requires"),
                   fx))
            fl.append('<package pkgid="%s" name=%s arch="%s">%s%s</package>' % (pkgid, quoteattr(name), arch, pv, fx))
            oth.append('<package pkgid="%s" name=%s arch="%s">%s</package>' % (pkgid, quoteattr(name), arch, pv))
    n = len(prim)
    docs = {
        "primary": '<?xml version="1.0" encoding="UTF-8"?>\n<metadata xmlns="http://linux.duke.edu/metadata/common" '
                   'xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">%s</metadata>\n' % (n, "".join(prim)),
        "filelists": '<?xml version="1.0" encoding="UTF-8"?>\n<filelists xmlns="http://linux.duke.edu/metadata/filelists" '
                     'packages="%d">%s</filelists>\n' % (n, "".join(fl)),
        "other": '<?xml version="1.0" encoding="UTF-8"?>\n<otherdata xmlns="http://linux.duke.edu/metadata/other" '
                 'packages="%d">%s</otherdata>\n' % (n, "".join(oth)),
    }
    rd = os.path.join(top, "repodata")
    os.makedirs(rd, exist_ok=True)
    for f in os.listdir(rd):
        os.remove(os.path.join(rd, f))
    now = int(time.time())
    md = []
    for t, x in docs.items():
        raw = x.encode()
        gz = gzip.compress(raw)
        fn = "%s.xml.gz" % t
        open(os.path.join(rd, fn), "wb").write(gz)
        md.append('<data type="%s"><checksum type="sha256">%s</checksum><open-checksum type="sha256">%s</open-checksum>'
                  '<location href="repodata/%s"/><timestamp>%d</timestamp><size>%d</size><open-size>%d</open-size></data>'
                  % (t, hashlib.sha256(gz).hexdigest(), hashlib.sha256(raw).hexdigest(), fn, now, len(gz), len(raw)))
    open(os.path.join(rd, "repomd.xml"), "w").write(
        '<?xml version="1.0" encoding="UTF-8"?>\n<repomd xmlns="http://linux.duke.edu/metadata/repo" '
        'xmlns:rpm="http://linux.duke.edu/metadata/rpm"><revision>%d</revision>%s</repomd>\n' % (now, "".join(md)))
    print("repodata: %d packages -> %s" % (n, rd))


if __name__ == "__main__":
    main(sys.argv[1])
