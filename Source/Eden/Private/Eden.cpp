/*
    Eden for Unreal Engine
    Copyright (C) 2026 LukaKrajina

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
#include "Eden.h"
#include "Interfaces/IPluginManager.h"
#include "Misc/Paths.h"
#include "HAL/PlatformProcess.h"

#define LOCTEXT_NAMESPACE "FEdenModule"

void* FEdenModule::AdamDllHandle = nullptr;

void FEdenModule::StartupModule()
{
	FString BaseDir = IPluginManager::Get().FindPlugin("Eden")->GetBaseDir();
	FString DllPath = FPaths::Combine(*BaseDir, TEXT("Binaries/Win64/UE5adam.dll"));

	AdamDllHandle = FPlatformProcess::GetDllHandle(*DllPath);
	if (!AdamDllHandle)
	{
		UE_LOG(LogTemp, Error, TEXT("[Eden] Failed to load UE5adam.dll from %s"), *DllPath);
	}
	else
	{
		UE_LOG(LogTemp, Log, TEXT("[Eden] Successfully loaded UE5adam.dll bridge"));
	}
}

void FEdenModule::ShutdownModule()
{
	if (AdamDllHandle)
	{
		FPlatformProcess::FreeDllHandle(AdamDllHandle);
		AdamDllHandle = nullptr;
	}
}

#undef LOCTEXT_NAMESPACE
IMPLEMENT_MODULE(FEdenModule, Eden)