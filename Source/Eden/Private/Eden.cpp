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