# -*- coding: utf-8 -*-
# 精确修复：ta=$\('input') 里的字面反斜杠（repr 显示 \$\( = $ + \( 两字符）
import io
p = "internal/server/static/index.html"
s = io.open(p, encoding="utf-8").read()
BS = chr(92)
LP = chr(40)  # (
bad = "$" + BS + LP + "'input')"
good = "$" + LP + "'input')"
n = s.count(bad)
s = s.replace(bad, good)
io.open(p, "w", encoding="utf-8").write(s)
print("fixed", n)
