#!/usr/bin/env python3
"""zipdir.py OUT.zip DIR [NAME...]: zips the entries NAME of DIR (all of
them by default), keeping Unix modes and symlinks, as an app bundle
needs; for systems without zip(1) or ditto(1)."""
import os, stat, sys, zipfile

out, base, names = sys.argv[1], sys.argv[2], sys.argv[3:] or sorted(os.listdir(sys.argv[2]))

def add(z, path, arc):
    st = os.lstat(path)
    if stat.S_ISLNK(st.st_mode):
        zi = zipfile.ZipInfo(arc)
        zi.create_system, zi.external_attr = 3, (stat.S_IFLNK | 0o777) << 16
        z.writestr(zi, os.readlink(path))
    elif stat.S_ISDIR(st.st_mode):
        zi = zipfile.ZipInfo(arc + "/")
        zi.create_system, zi.external_attr = 3, ((stat.S_IFDIR | stat.S_IMODE(st.st_mode)) << 16) | 0x10
        z.writestr(zi, b"")
        for n in sorted(os.listdir(path)):
            add(z, os.path.join(path, n), arc + "/" + n)
    else:
        zi = zipfile.ZipInfo.from_file(path, arc)
        zi.create_system, zi.compress_type = 3, zipfile.ZIP_DEFLATED
        zi.external_attr = (stat.S_IFREG | stat.S_IMODE(st.st_mode)) << 16
        with open(path, "rb") as f:
            z.writestr(zi, f.read())

with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for n in names:
        add(z, os.path.join(base, n), n)
