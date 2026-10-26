@echo off
rem Voyage launcher v4: fresh page on every launch
rem  v2 两处根源缺陷已修：①关窗按进程命令行匹配不到（App 窗口无独立进程）
rem                     → 改 Win32 窗口标题枚举 close_voyage_windows.ps1
rem  ②URL 无版本参数，Edge 磁盘缓存直接吃旧 HTML → URL 带随机戳打破缓存键
cd /d "%~dp0"
netstat -ano | findstr /C:"127.0.0.1:7412" | findstr /C:"LISTENING" >nul 2>&1
if not errorlevel 1 goto reuse
start "voyage" /min outpost2.exe serve --no-open
setlocal enabledelayedexpansion
set /a tries=0
:waitloop
ping -n 2 127.0.0.1 >nul
curl -s -o nul http://127.0.0.1:7412/api/state
if errorlevel 1 (
  set /a tries+=1
  if !tries! LSS 15 goto waitloop
)
endlocal
:reuse
rem close stale voyage windows by title (App 窗口无独立进程，标题枚举才关得掉)
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0tools\close_voyage_windows.ps1"
ping -n 2 127.0.0.1 >nul
set "EDGE=%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe"
if not exist "%EDGE%" set "EDGE=%ProgramFiles%\Google\Chrome\Application\chrome.exe"
set "TS=%RANDOM%%RANDOM%%RANDOM%"
start "" "%EDGE%" --app=http://localhost:7412/?v=%TS%
exit /b 0
