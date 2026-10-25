@echo off
rem Voyage launcher: correct workdir, single instance, native-feel app window
cd /d "%~dp0"
netstat -ano | findstr /C:"127.0.0.1:7412" | findstr /C:"LISTENING" >nul 2>&1
if not errorlevel 1 goto openapp
start "voyage" /min outpost2.exe serve --no-open
setlocal enabledelayedexpansion
set /a tries=0
:waitloop
timeout /t 1 >nul
curl -s -o nul http://127.0.0.1:7412/api/state
if errorlevel 1 (
  set /a tries+=1
  if !tries! LSS 10 goto waitloop
)
endlocal
:openapp
set "EDGE=%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe"
if exist "%EDGE%" (
  start "" "%EDGE%" --app=http://127.0.0.1:7412
) else if exist "%ProgramFiles%\Google\Chrome\Application\chrome.exe" (
  start "" "%ProgramFiles%\Google\Chrome\Application\chrome.exe" --app=http://127.0.0.1:7412
) else (
  start "" http://127.0.0.1:7412
)
exit /b 0
