#!/usr/bin/env python3
"""Проверяем прямые импорты по ТЗ; gocui явно разрешён для терминального интерфейса."""
import json
import subprocess

allowed = {"io", "log", "os", "fmt", "net", "sync", "time", "bufio", "errors", "strings", "reflect"}
module = "github.com/ernat-soltanbekov/net-link"
text = subprocess.check_output(["go", "list", "-json", "./..."], text=True)
decoder = json.JSONDecoder()
packages = 0
while text.strip():
    package, end = decoder.raw_decode(text.lstrip())
    text = text.lstrip()[end:]
    for field in ("Imports", "TestImports", "XTestImports"):
        for name in package.get(field, []):
            own = name == module or name.startswith(module + "/")
            test = field != "Imports" and name == "testing"
            tui = name == "github.com/jroimartin/gocui" and package["ImportPath"] == module + "/client"
            assert own or test or tui or name in allowed, (package["ImportPath"], field, name)
    packages += 1
print("PASS: allowed direct imports in %d packages; testing and the specified gocui exception only" % packages)
