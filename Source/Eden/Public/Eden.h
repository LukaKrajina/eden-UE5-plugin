#pragma once

#include "Modules/ModuleManager.h"

class FEdenModule : public IModuleInterface
{
public:
	virtual void StartupModule() override;
	virtual void ShutdownModule() override;
    
	static void* AdamDllHandle;
};