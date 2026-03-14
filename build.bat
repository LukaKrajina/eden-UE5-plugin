@echo off
setlocal enabledelayedexpansion

echo ========================================================
echo Building Eden DLLs
echo ========================================================

:: Define absolute paths relative to the batch script
set "CORE_DIR=%~dp0core"
set "BIN_DIR=%~dp0Binaries\Win64"

:: Create the target binaries directory if it doesn't exist
if not exist "%BIN_DIR%" (
    echo Creating binaries directory...
    mkdir "%BIN_DIR%"
)

echo.
echo [1/2] Compiling Go files (mainUE.go, eveUE.go) into UE5cain.dll...
:: Compile to c-shared and explicitly name it UE5cain.dll as required by adamUE.cpp
go build -buildmode=c-shared -o "%BIN_DIR%\UE5cain.dll" "%CORE_DIR%\mainUE.go" "%CORE_DIR%\eveUE.go"

if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Go compilation failed. Please check the errors above.
    exit /b %errorlevel%
)
echo - Go compilation successful!

echo.
echo [2/2] Compiling C++ file (adamUE.cpp) into UE5adam.dll...
:: Use cl.exe. We point the include path (/I) to core, and output to the BIN_DIR
:: Note: This assumes wintun.lib is accessible in the core folder or system LIB path.
pushd "%CORE_DIR%"
cl.exe /O2 /LD /W3 /I. "adamUE.cpp" /Fe"%BIN_DIR%\UE5adam.dll" /Fo"%BIN_DIR%\UE5adam.obj" /link /LIBPATH:.
popd

if %errorlevel% neq 0 (
    echo.
    echo [ERROR] C++ compilation failed. Ensure you are running this in a Developer Command Prompt and wintun.lib is present.
    exit /b %errorlevel%
)
echo - C++ compilation successful!

echo.
echo Cleaning up intermediate build files...
del "%BIN_DIR%\*.obj" 2>nul
del "%BIN_DIR%\*.exp" 2>nul
:: The Go compiler generates a .h file when making a c-shared DLL, it can safely delete.
del "%BIN_DIR%\UE5cain.h" 2>nul

echo.
echo ========================================================
echo Build Complete! Your DLLs are ready in:
echo %BIN_DIR%
echo ========================================================
pause